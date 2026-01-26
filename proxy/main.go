package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Config struct {
	ListenAddr        string
	UpstreamBaseURL   string
	UpstreamAPIKey    string
	CollectorURL      string
	EventQueueSize    int
	EventFlushTimeout time.Duration
	HTTPClientTimeout time.Duration

	// Best-effort metering parse: capture first N bytes of upstream response
	MeteringCaptureBytes int
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIResponse struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage *Usage `json:"usage"`
}

type OpenAIRequest struct {
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Messages any    `json:"messages"` // MVP
}

type MeteringEvent struct {
	RequestID        string    `json:"request_id"`
	Tenant           string    `json:"tenant"`
	AppKey           string    `json:"app_key"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	LatencyMs        int64     `json:"latency_ms"`
	StatusCode       int       `json:"status_code"`
	At               time.Time `json:"ts"`
}

type StreamChunk struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	Usage *Usage `json:"usage"`
}

func loadConfig() (Config, error) {
	cfg := Config{
		ListenAddr:           envOr("LISTEN_ADDR", ":8080"),
		UpstreamBaseURL:      envOr("UPSTREAM_OPENAI_BASE_URL", "https://api.openai.com"),
		UpstreamAPIKey:       os.Getenv("UPSTREAM_OPENAI_API_KEY"),
		CollectorURL:         envOr("COLLECTOR_URL", "http://llm-collector.llm-system.svc.cluster.local:8081/events"),
		EventQueueSize:       envOrInt("EVENT_QUEUE_SIZE", 10000),
		EventFlushTimeout:    envOrDuration("EVENT_FLUSH_TIMEOUT", 2*time.Second),
		HTTPClientTimeout:    envOrDuration("HTTP_CLIENT_TIMEOUT", 120*time.Second),
		MeteringCaptureBytes: envOrInt("METERING_CAPTURE_BYTES", 256*1024), // 256KB
	}

	if cfg.UpstreamAPIKey == "" {
		return cfg, errors.New("UPSTREAM_OPENAI_API_KEY is required")
	}
	if cfg.MeteringCaptureBytes < 0 {
		cfg.MeteringCaptureBytes = 0
	}
	return cfg, nil
}

func streamSSE(w http.ResponseWriter, upstream io.Reader) (string, *Usage, error) {
	br := bufio.NewReaderSize(upstream, 32*1024)

	var model string
	var usage *Usage

	var fl http.Flusher
	if f, ok := w.(http.Flusher); ok {
		fl = f
	}

	for {
		line, err := br.ReadBytes('\n') // SSE is line-oriented

		if len(line) > 0 {
			// 1) forward raw bytes as-is
			if _, werr := w.Write(line); werr != nil {
				return model, usage, werr // client disconnected etc.
			}
			if fl != nil {
				fl.Flush()
			}

			// 2) best-effort parse: data: ...
			trim := bytes.TrimSpace(line)
			if bytes.HasPrefix(trim, []byte("data:")) {
				payload := bytes.TrimSpace(bytes.TrimPrefix(trim, []byte("data:")))

				// stop marker
				if bytes.Equal(payload, []byte("[DONE]")) {
					return model, usage, nil
					// done, but still continue reading until EOF just in case
				} else if len(payload) > 0 && payload[0] == '{' {
					var ch StreamChunk
					if jsonErr := json.Unmarshal(payload, &ch); jsonErr == nil {
						if ch.Model != "" {
							model = ch.Model
						}
						// usage only appears if client enabled stream_options.include_usage
						if ch.Usage != nil {
							usage = ch.Usage
						}
					}
				}
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				return model, usage, nil
			}
			return model, usage, err
		}
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	// Upstream HTTP client (keep-alive)
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   200,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	upstreamClient := &http.Client{
		Timeout:   cfg.HTTPClientTimeout,
		Transport: transport,
	}

	// Collector client: short timeout to avoid hanging background sender
	collectorClient := &http.Client{
		Timeout:   800 * time.Millisecond,
		Transport: transport,
	}

	events := make(chan MeteringEvent, cfg.EventQueueSize)
	var dropped uint64

	// Background sender (MVP: one-by-one; later: batch+gzip/protobuf)
	go func() {
		ticker := time.NewTicker(cfg.EventFlushTimeout)
		defer ticker.Stop()

		for {
			select {
			case ev := <-events:
				if err := postEvent(collectorClient, cfg.CollectorURL, ev); err != nil {
					// fail-open: drop on error
					log.Printf("collector post failed (drop): %v", err)
				}
			case <-ticker.C:
				d := atomic.LoadUint64(&dropped)
				if d > 0 {
					log.Printf("metering: dropped_events=%d (queue full or overload)", d)
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// MVP endpoint: Chat Completions
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		start := time.Now()
		requestID := newReqID()

		tenant := firstNonEmpty(
			r.Header.Get("X-LLM-Tenant"),
			r.Header.Get("X-Tenant"),
			"default",
		)

		appKey := bearerToken(r.Header.Get("Authorization"))
		if appKey == "" {
			http.Error(w, "missing Authorization bearer token (gateway key)", http.StatusUnauthorized)
			return
		}

		// Read request body (kept for MVP). Next iteration can stream request too.
		reqBody, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()

		// Parse minimal fields from request (model/stream)
		var oreq OpenAIRequest
		_ = json.Unmarshal(reqBody, &oreq)

		// Build upstream request
		upURL := strings.TrimRight(cfg.UpstreamBaseURL, "/") + "/v1/chat/completions"
		upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upURL, bytes.NewReader(reqBody))
		if err != nil {
			http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
			return
		}
		upReq.Header.Set("Content-Type", "application/json")
		upReq.Header.Set("Authorization", "Bearer "+cfg.UpstreamAPIKey)

		// Forward some optional headers if needed
		if org := r.Header.Get("OpenAI-Organization"); org != "" {
			upReq.Header.Set("OpenAI-Organization", org)
		}
		if beta := r.Header.Get("OpenAI-Beta"); beta != "" {
			upReq.Header.Set("OpenAI-Beta", beta)
		}
		if proj := r.Header.Get("OpenAI-Project"); proj != "" {
			upReq.Header.Set("OpenAI-Project", proj)
		}

		upResp, err := upstreamClient.Do(upReq)
		if err != nil {
			http.Error(w, "upstream request failed", http.StatusBadGateway)

			// Emit metering event even on upstream failure (status_code=0)
			ev := MeteringEvent{
				RequestID:        requestID,
				Tenant:           tenant,
				AppKey:           appKey,
				Provider:         "openai",
				Model:            firstNonEmpty(oreq.Model, "unknown"),
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				LatencyMs:        time.Since(start).Milliseconds(),
				StatusCode:       0,
				At:               time.Now().UTC(),
			}
			enqueueEvent(events, &dropped, ev)
			return
		}
		defer upResp.Body.Close()

		// Copy headers (filter hop-by-hop)
		for k, vals := range upResp.Header {
			if isHopByHopHeader(k) {
				continue
			}
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		// Add our own trace id to help debugging
		w.Header().Set("X-LLM-Request-ID", requestID)

		// Write status code immediately
		w.WriteHeader(upResp.StatusCode)

		if oreq.Stream {
			seenModel, seenUsage, copyErr := streamSSE(w, upResp.Body)
			lat := time.Since(start)

			model := firstNonEmpty(seenModel, oreq.Model, "unknown")

			ev := MeteringEvent{
				RequestID:        requestID,
				Tenant:           tenant,
				AppKey:           appKey,
				Provider:         "openai",
				Model:            model,
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
				LatencyMs:        lat.Milliseconds(),
				StatusCode:       upResp.StatusCode,
				At:               time.Now().UTC(),
			}
			if seenUsage != nil {
				ev.PromptTokens = seenUsage.PromptTokens
				ev.CompletionTokens = seenUsage.CompletionTokens
				ev.TotalTokens = seenUsage.TotalTokens
			}

			enqueueEvent(events, &dropped, ev)

			if copyErr != nil {
				log.Printf("proxy stream copy error request_id=%s status=%d err=%v", requestID, upResp.StatusCode, copyErr)
			}
			return
		}

		// We want: stream upstream -> client WITHOUT buffering.
		// Additionally: capture first N bytes for best-effort usage parsing.
		capWriter := newLimitedCapture(cfg.MeteringCaptureBytes)
		tee := io.TeeReader(upResp.Body, capWriter)

		// Ensure streaming to client flushes when possible
		var out io.Writer = w
		if fl, ok := w.(http.Flusher); ok {
			out = &flushWriter{w: w, fl: fl}
		}

		_, copyErr := io.Copy(out, tee)
		lat := time.Since(start)

		// Best-effort parse usage from captured bytes (may fail if truncated)
		captured := capWriter.Bytes()
		var oresp OpenAIResponse
		if len(captured) > 0 {
			_ = json.Unmarshal(captured, &oresp)
		}

		model := firstNonEmpty(oresp.Model, oreq.Model, "unknown")

		ev := MeteringEvent{
			RequestID:        requestID,
			Tenant:           tenant,
			AppKey:           appKey,
			Provider:         "openai",
			Model:            model,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			LatencyMs:        lat.Milliseconds(),
			StatusCode:       upResp.StatusCode,
			At:               time.Now().UTC(),
		}
		if oresp.Usage != nil {
			ev.PromptTokens = oresp.Usage.PromptTokens
			ev.CompletionTokens = oresp.Usage.CompletionTokens
			ev.TotalTokens = oresp.Usage.TotalTokens
		}

		enqueueEvent(events, &dropped, ev)

		// If copy failed, log it (client might have disconnected)
		if copyErr != nil {
			log.Printf("proxy copy error request_id=%s status=%d err=%v", requestID, upResp.StatusCode, copyErr)
		}
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0, // response can be long; keep 0 for now
		IdleTimeout:       90 * time.Second,
	}

	log.Printf("llm-proxy listening on %s (upstream=%s collector=%s capture_bytes=%d)",
		cfg.ListenAddr, cfg.UpstreamBaseURL, cfg.CollectorURL, cfg.MeteringCaptureBytes)

	log.Fatal(srv.ListenAndServe())
}

func enqueueEvent(ch chan MeteringEvent, dropped *uint64, ev MeteringEvent) {
	select {
	case ch <- ev:
	default:
		atomic.AddUint64(dropped, 1)
	}
}

func postEvent(client *http.Client, collectorURL string, ev MeteringEvent) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, collectorURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return errors.New("collector returned " + resp.Status + " body=" + string(body))
	}
	return nil
}

func newReqID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "req_" + hex.EncodeToString(b[:])
	}
	return "req_fallback_" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

func envOr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func envOrInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func envOrDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func bearerToken(auth string) string {
	auth = strings.TrimSpace(auth)
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func isHopByHopHeader(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

// flushWriter flushes after each Write to reduce buffering and improve TTFB.
// This does not change the response format; clients still see a single JSON body.
type flushWriter struct {
	w  http.ResponseWriter
	fl http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.fl.Flush()
	return n, err
}

// limitedCapture collects up to N bytes written to it, ignoring the rest.
// Used to "tee" upstream response for best-effort JSON usage parsing without buffering everything.
type limitedCapture struct {
	limit int
	buf   []byte
}

func newLimitedCapture(limit int) *limitedCapture {
	if limit <= 0 {
		return &limitedCapture{limit: 0, buf: nil}
	}
	return &limitedCapture{limit: limit, buf: make([]byte, 0, min(limit, 16*1024))}
}

func (lc *limitedCapture) Write(p []byte) (int, error) {
	if lc.limit <= 0 {
		return len(p), nil
	}
	remain := lc.limit - len(lc.buf)
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) <= remain {
		lc.buf = append(lc.buf, p...)
		return len(p), nil
	}
	lc.buf = append(lc.buf, p[:remain]...)
	return len(p), nil
}

func (lc *limitedCapture) Bytes() []byte {
	return lc.buf
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
