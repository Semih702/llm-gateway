package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

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
	Stream           bool      `json:"stream,omitempty"`
}

type Server struct {
	mu   sync.Mutex
	file *os.File
	w    *bufio.Writer
}

func main() {
	// Default: 8081 to match README/k8s manifests
	addr := ":" + getenv("PORT", "8081")
	outPath := getenv("EVENT_LOG_PATH", "") // e.g. /data/events.ndjson

	s := &Server{}
	if outPath != "" {
		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("open log file: %v", err)
		}
		s.file = f
		s.w = bufio.NewWriterSize(f, 1<<20)
		defer func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			_ = s.w.Flush()
			_ = s.file.Close()
		}()
		log.Printf("collector: writing events to %s", outPath)
	} else {
		log.Printf("collector: EVENT_LOG_PATH not set; events will be printed to stdout")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/events", s.handleEvents)

	log.Printf("collector listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var ev MeteringEvent
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // strict for safety
	if err := dec.Decode(&ev); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Minimal validation
	if ev.RequestID == "" {
		http.Error(w, "missing request_id", http.StatusBadRequest)
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}

	// Persist: NDJSON append OR stdout
	b, err := json.Marshal(ev)
	if err != nil {
		http.Error(w, "failed to marshal", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.w != nil {
		_, _ = s.w.Write(b)
		_, _ = s.w.WriteString("\n")
		_ = s.w.Flush()
	} else {
		log.Printf("EVENT %s", string(b))
	}

	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("accepted"))
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}
