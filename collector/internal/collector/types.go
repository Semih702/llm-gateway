package collector

import "time"

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
