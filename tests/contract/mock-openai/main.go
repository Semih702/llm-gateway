package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type ChatReq struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req ChatReq
		_ = json.NewDecoder(r.Body).Decode(&req)

		// STREAM: SSE
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			fmt.Fprintf(w, "data: {\"id\":\"mock-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n")
			w.(http.Flusher).Flush()

			fmt.Fprintf(w, "data: {\"id\":\"mock-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello-from-mock\"}}]}\n\n")
			w.(http.Flusher).Flush()

			time.Sleep(5 * time.Millisecond)

			fmt.Fprintf(w, "data: [DONE]\n\n")
			w.(http.Flusher).Flush()
			return
		}

		w.Header().Set("Content-Type", "application/json")

		resp := map[string]any{
			"id":      "mock-1",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   req.Model,
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok-from-mock",
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     7,
				"completion_tokens": 3,
				"total_tokens":      10,
			},
		}

		_ = json.NewEncoder(w).Encode(resp)
	})

	log.Println("mock-openai listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
