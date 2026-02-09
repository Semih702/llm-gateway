package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBearerToken(t *testing.T) {
	require.Equal(t, "abc", BearerToken("Bearer abc"))
	require.Equal(t, "", BearerToken("Basic abc"))
	require.Equal(t, "", BearerToken(""))
}

func TestFirstNonEmpty(t *testing.T) {
	require.Equal(t, "b", FirstNonEmpty("", "b", "c"))
	require.Equal(t, "", FirstNonEmpty("", " "))
}

func TestIsHopByHopHeader(t *testing.T) {
	require.True(t, IsHopByHopHeader("Connection"))
	require.False(t, IsHopByHopHeader("Content-Type"))
}
