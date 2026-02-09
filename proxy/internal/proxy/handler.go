package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type Server struct {
	cfg             Config
	upstreamClient  *http.Client
	collectorClient *http.Client

	events  chan MeteringEvent
	dropped uint64
}

func NewServer(cfg Config) *Server {
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

	s := &Server{
		cfg: cfg,
		upstreamClient: &http.Client{
			Timeout:   cfg.HTTPClientTimeout,
			Transport: transport,
		},
		collectorClient: &http.Client{
			Timeout:   800 * time.Millisecond,
			Transport: transport,
		},
		events: make(chan MeteringEvent, cfg.EventQueueSize),
	}

	go s.backgroundSender()

	return s
}

func (s *Server) backgroundSender() {
	ticker := time.NewTicker(s.cfg.EventFlushTimeout)
	defer ticker.Stop()

	for {
		select {
		case ev := <-s.events:
			if err := postEvent(s.collectorClient, s.cfg.CollectorURL, ev); err != nil {
				log.Printf("collector post failed (drop): %v", err)
			}
		case <-ticker.C:
			d := atomic.LoadUint64(&s.dropped)
			if d > 0 {
				log.Printf("metering: dropped_events=%d (queue full or overload)", d)
			}
		}
	}
}

func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)

	return mux
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()
	requestID := NewReqID()

	tenant := FirstNonEmpty(
		r.Header.Get("X-LLM-Tenant"),
		r.Header.Get("X-Tenant"),
		"default",
	)

	appKey := BearerToken(r.Header.Get("Authorization"))
	if appKey == "" {
		http.Error(w, "missing Authorization bearer token (gateway key)", http.StatusUnauthorized)
		return
	}

	reqBody, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	var oreq OpenAIRequest
	_ = json.Unmarshal(reqBody, &oreq)

	upURL := strings.TrimRight(s.cfg.UpstreamBaseURL, "/") + "/v1/chat/completions"
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upURL, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "failed to create upstream request", http.StatusInternalServerError)
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Authorization", "Bearer "+s.cfg.UpstreamAPIKey)

	if org := r.Header.Get("OpenAI-Organization"); org != "" {
		upReq.Header.Set("OpenAI-Organization", org)
	}
	if beta := r.Header.Get("OpenAI-Beta"); beta != "" {
		upReq.Header.Set("OpenAI-Beta", beta)
	}
	if proj := r.Header.Get("OpenAI-Project"); proj != "" {
		upReq.Header.Set("OpenAI-Project", proj)
	}

	upResp, err := s.upstreamClient.Do(upReq)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		s.enqueue(MeteringEvent{
			RequestID:        requestID,
			Tenant:           tenant,
			AppKey:           appKey,
			Provider:         "openai",
			Model:            FirstNonEmpty(oreq.Model, "unknown"),
			LatencyMs:        time.Since(start).Milliseconds(),
			StatusCode:       0,
			At:               time.Now().UTC(),
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		})
		return
	}
	defer upResp.Body.Close()

	for k, vals := range upResp.Header {
		if IsHopByHopHeader(k) {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-LLM-Request-ID", requestID)
	w.WriteHeader(upResp.StatusCode)

	if oreq.Stream {
		seenModel, seenUsage, copyErr := StreamSSE(w, upResp.Body)
		lat := time.Since(start)

		model := FirstNonEmpty(seenModel, oreq.Model, "unknown")
		ev := MeteringEvent{
			RequestID:        requestID,
			Tenant:           tenant,
			AppKey:           appKey,
			Provider:         "openai",
			Model:            model,
			LatencyMs:        lat.Milliseconds(),
			StatusCode:       upResp.StatusCode,
			At:               time.Now().UTC(),
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
		}
		if seenUsage != nil {
			ev.PromptTokens = seenUsage.PromptTokens
			ev.CompletionTokens = seenUsage.CompletionTokens
			ev.TotalTokens = seenUsage.TotalTokens
		}
		s.enqueue(ev)

		if copyErr != nil {
			log.Printf("proxy stream copy error request_id=%s status=%d err=%v", requestID, upResp.StatusCode, copyErr)
		}
		return
	}

	capWriter := NewLimitedCapture(s.cfg.MeteringCaptureBytes)
	tee := io.TeeReader(upResp.Body, capWriter)

	var out io.Writer = w
	if fl, ok := w.(http.Flusher); ok {
		out = &flushWriter{w: w, fl: fl}
	}

	_, copyErr := io.Copy(out, tee)
	lat := time.Since(start)

	captured := capWriter.Bytes()
	var oresp OpenAIResponse
	if len(captured) > 0 {
		_ = json.Unmarshal(captured, &oresp)
	}

	model := FirstNonEmpty(oresp.Model, oreq.Model, "unknown")

	ev := MeteringEvent{
		RequestID:        requestID,
		Tenant:           tenant,
		AppKey:           appKey,
		Provider:         "openai",
		Model:            model,
		LatencyMs:        lat.Milliseconds(),
		StatusCode:       upResp.StatusCode,
		At:               time.Now().UTC(),
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}
	if oresp.Usage != nil {
		ev.PromptTokens = oresp.Usage.PromptTokens
		ev.CompletionTokens = oresp.Usage.CompletionTokens
		ev.TotalTokens = oresp.Usage.TotalTokens
	}
	s.enqueue(ev)

	if copyErr != nil {
		log.Printf("proxy copy error request_id=%s status=%d err=%v", requestID, upResp.StatusCode, copyErr)
	}
}

func (s *Server) enqueue(ev MeteringEvent) {
	select {
	case s.events <- ev:
	default:
		atomic.AddUint64(&s.dropped, 1)
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

type flushWriter struct {
	w  http.ResponseWriter
	fl http.Flusher
}

func (fw *flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	fw.fl.Flush()
	return n, err
}
