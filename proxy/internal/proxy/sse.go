package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func StreamSSE(w http.ResponseWriter, upstream io.Reader) (string, *Usage, error) {
	br := bufio.NewReaderSize(upstream, 32*1024)

	var model string
	var usage *Usage

	var fl http.Flusher
	if f, ok := w.(http.Flusher); ok {
		fl = f
	}

	for {
		line, err := br.ReadBytes('\n')

		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				return model, usage, werr
			}
			if fl != nil {
				fl.Flush()
			}

			trim := bytes.TrimSpace(line)
			if bytes.HasPrefix(trim, []byte("data:")) {
				payload := bytes.TrimSpace(bytes.TrimPrefix(trim, []byte("data:")))

				if bytes.Equal(payload, []byte("[DONE]")) {
					return model, usage, nil
				}
				if len(payload) > 0 && payload[0] == '{' {
					var ch StreamChunk
					if jsonErr := json.Unmarshal(payload, &ch); jsonErr == nil {
						if ch.Model != "" {
							model = ch.Model
						}
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
