package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"time"
)

func NewReqID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return "req_" + hex.EncodeToString(b[:])
	}
	return "req_fallback_" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

func EnvOr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func EnvOrInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func EnvOrDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func BearerToken(auth string) string {
	auth = strings.TrimSpace(auth)
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func IsHopByHopHeader(k string) bool {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
		"te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
