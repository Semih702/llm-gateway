package proxy

import "time"

type Config struct {
	ListenAddr        string
	UpstreamBaseURL   string
	UpstreamAPIKey    string
	CollectorURL      string
	EventQueueSize    int
	EventFlushTimeout time.Duration
	HTTPClientTimeout time.Duration

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
	Messages any    `json:"messages"`
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
