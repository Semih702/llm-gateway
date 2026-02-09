package main

import (
	"log"
	"net/http"
	"time"

	proxy "llm-proxy/internal/proxy"
)

func main() {
	cfg, err := proxy.LoadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	s := proxy.NewServer(cfg)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           s.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       90 * time.Second,
	}

	log.Printf("llm-proxy listening on %s (upstream=%s collector=%s capture_bytes=%d)",
		cfg.ListenAddr, cfg.UpstreamBaseURL, cfg.CollectorURL, cfg.MeteringCaptureBytes)

	log.Fatal(srv.ListenAndServe())
}
