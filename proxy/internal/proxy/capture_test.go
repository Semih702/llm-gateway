package proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLimitedCapture_RespectsLimit(t *testing.T) {
	lc := NewLimitedCapture(5)
	n, err := lc.Write([]byte("hello world"))
	require.NoError(t, err)
	require.Equal(t, 11, n)
	require.Equal(t, []byte("hello"), lc.Bytes())
}

func TestLimitedCapture_ZeroLimit(t *testing.T) {
	lc := NewLimitedCapture(0)
	_, err := lc.Write([]byte("data"))
	require.NoError(t, err)
	require.Nil(t, lc.Bytes())
}
