package main

import (
	"log"
	"net/http"
	"time"

	collector "llm-collector/internal/collector"
)

func main() {
	addr := ":" + collector.Getenv("PORT", "8081")
	outPath := collector.Getenv("EVENT_LOG_PATH", "")

	s, err := collector.NewServer(outPath)
	if err != nil {
		log.Fatalf("open log file: %v", err)
	}
	defer s.Close()

	log.Printf("collector listening on %s", addr)

	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}
