package proxy

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamSSE_MultiChunkWithUsage(t *testing.T) {
	input := `
data: {"id":"1","model":"gpt-4","usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}

data: [DONE]
`
	rec := httptest.NewRecorder()

	model, usage, err := StreamSSE(rec, bytes.NewBufferString(input))
	require.NoError(t, err)
	require.Equal(t, "gpt-4", model)
	require.NotNil(t, usage)
	require.Equal(t, 12, usage.TotalTokens)
	require.Contains(t, rec.Body.String(), "data:")
}

func TestStreamSSE_NoUsage(t *testing.T) {
	input := `
data: {"id":"1","model":"gpt-3.5"}

data: [DONE]
`
	rec := httptest.NewRecorder()

	model, usage, err := StreamSSE(rec, bytes.NewBufferString(input))
	require.NoError(t, err)
	require.Equal(t, "gpt-3.5", model)
	require.Nil(t, usage)
}
