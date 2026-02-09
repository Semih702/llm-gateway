package collector

import (
	"bufio"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type Server struct {
	mu   sync.Mutex
	file *os.File
	w    *bufio.Writer
}

// NewServer creates a server. If outPath is empty, events are printed to stdout.
func NewServer(outPath string) (*Server, error) {
	s := &Server{}
	if outPath == "" {
		log.Printf("collector: EVENT_LOG_PATH not set; events will be printed to stdout")
		return s, nil
	}

	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	s.file = f
	s.w = bufio.NewWriterSize(f, 1<<20)
	log.Printf("collector: writing events to %s", outPath)
	return s, nil
}

// Close flushes and closes the underlying file if configured.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.w != nil {
		_ = s.w.Flush()
	}
	if s.file != nil {
		_ = s.file.Close()
	}
}

func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/events", s.HandleEvents)
	return mux
}

func (s *Server) HandleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var ev MeteringEvent
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ev); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if ev.RequestID == "" {
		http.Error(w, "missing request_id", http.StatusBadRequest)
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}

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
