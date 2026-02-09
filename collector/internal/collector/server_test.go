package collector

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandleEvents_Accepted(t *testing.T) {
	s, err := NewServer("") // stdout mode
	require.NoError(t, err)

	ev := MeteringEvent{
		RequestID: "req_123",
		Tenant:    "default",
		Provider:  "openai",
		Model:     "gpt-4",
		At:        time.Now().UTC(),
	}

	body, _ := json.Marshal(ev)
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.HandleEvents(rec, req)

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Contains(t, rec.Body.String(), "accepted")
}

func TestHandleEvents_InvalidJSON(t *testing.T) {
	s, err := NewServer("")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString("{bad json"))
	rec := httptest.NewRecorder()

	s.HandleEvents(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleEvents_MissingRequestID(t *testing.T) {
	s, err := NewServer("")
	require.NoError(t, err)

	ev := MeteringEvent{}
	body, _ := json.Marshal(ev)

	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	s.HandleEvents(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
