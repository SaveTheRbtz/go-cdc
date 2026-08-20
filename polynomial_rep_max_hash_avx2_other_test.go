//go:build !amd64 || purego

package cdc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPolyRepMaxContentDefinedChunkerAVX2Unavailable(t *testing.T) {
	require.Panics(t, func() {
		NewPolyRepMaxContentDefinedChunkerAVX2(polynomialHashWindowSizeBytes, 0)
	})
}
