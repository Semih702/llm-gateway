package proxy

import (
	"errors"
	"os"
	"time"
)

func LoadConfig() (Config, error) {
	cfg := Config{
		ListenAddr:           EnvOr("LISTEN_ADDR", ":8080"),
		UpstreamBaseURL:      EnvOr("UPSTREAM_OPENAI_BASE_URL", "https://api.openai.com"),
		UpstreamAPIKey:       os.Getenv("UPSTREAM_OPENAI_API_KEY"),
		CollectorURL:         EnvOr("COLLECTOR_URL", "http://llm-collector.llm-system.svc.cluster.local:8081/events"),
		EventQueueSize:       EnvOrInt("EVENT_QUEUE_SIZE", 10000),
		EventFlushTimeout:    EnvOrDuration("EVENT_FLUSH_TIMEOUT", 2*time.Second),
		HTTPClientTimeout:    EnvOrDuration("HTTP_CLIENT_TIMEOUT", 120*time.Second),
		MeteringCaptureBytes: EnvOrInt("METERING_CAPTURE_BYTES", 256*1024),
	}

	if cfg.UpstreamAPIKey == "" {
		return cfg, errors.New("UPSTREAM_OPENAI_API_KEY is required")
	}
	if cfg.MeteringCaptureBytes < 0 {
		cfg.MeteringCaptureBytes = 0
	}
	return cfg, nil
}
