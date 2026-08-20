package cdc_test

import (
	"bufio"
	"bytes"
	"math"
	"testing"

	"github.com/buildbarn/go-cdc"
	"github.com/stretchr/testify/require"
)

func ramChunkSize(data []byte, windowSizeBytes int) int {
	if len(data) <= windowSizeBytes {
		return len(data)
	}

	maximum := data[0]
	for _, b := range data[1:windowSizeBytes] {
		if b > maximum {
			maximum = b
		}
	}
	for i := windowSizeBytes; i < len(data); i++ {
		if data[i] >= maximum {
			return i + 1
		}
	}
	return len(data)
}

func TestRAMContentDefinedChunkerValidation(t *testing.T) {
	for _, windowSizeBytes := range []int{-1, 0, math.MaxInt} {
		require.Panics(t, func() {
			cdc.NewRAMContentDefinedChunker(windowSizeBytes)
		})
	}

	chunker := cdc.NewRAMContentDefinedChunker(1024)
	require.False(t, chunker.SupportsDiscardUpToGuaranteedChunk())
	require.Equal(t, 64*1024, chunker.GetMaximumPeekSizeBytes())
	require.Panics(t, func() {
		chunker.DiscardUpToGuaranteedChunk(bufio.NewReader(bytes.NewReader(nil)))
	})

	largeWindowChunker := cdc.NewRAMContentDefinedChunker(128 * 1024)
	require.Equal(t, 128*1024+1, largeWindowChunker.GetMaximumPeekSizeBytes())
}

func TestRAMLContentDefinedChunkerValidation(t *testing.T) {
	for _, windowSizeBytes := range []int{-1, 0, math.MaxInt/4 + 1, math.MaxInt} {
		require.Panics(t, func() {
			cdc.NewRAMLContentDefinedChunker(windowSizeBytes)
		})
	}

	chunker := cdc.NewRAMLContentDefinedChunker(1024)
	require.False(t, chunker.SupportsDiscardUpToGuaranteedChunk())
	require.Equal(t, 64*1024, chunker.GetMaximumPeekSizeBytes())
}

func TestRAMContentDefinedChunkerBoundaries(t *testing.T) {
	testCases := map[string]struct {
		windowSizeBytes int
		data            []byte
		expectedSizes   []int
	}{
		"Empty": {
			windowSizeBytes: 3,
		},
		"ShortFinalChunk": {
			windowSizeBytes: 3,
			data:            []byte{9, 1, 2},
			expectedSizes:   []int{3},
		},
		"ImmediateBoundary": {
			windowSizeBytes: 3,
			data:            []byte{3, 1, 2, 4, 8},
			expectedSizes:   []int{4, 1},
		},
		"EqualityQualifies": {
			windowSizeBytes: 3,
			data:            []byte{1, 9, 2, 8, 9, 4},
			expectedSizes:   []int{5, 1},
		},
		"MaximumByte": {
			windowSizeBytes: 3,
			data:            []byte{0xff, 1, 2, 8, 9, 0xff, 4},
			expectedSizes:   []int{6, 1},
		},
		"MinimumWindow": {
			windowSizeBytes: 1,
			data:            []byte{2, 1, 2, 3},
			expectedSizes:   []int{3, 1},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			chunker := cdc.NewRAMContentDefinedChunker(testCase.windowSizeBytes)
			chunks := chunkerChunks(t, chunker, testCase.data)
			var sizes []int
			for _, chunk := range chunks {
				sizes = append(sizes, len(chunk))
			}
			require.Equal(t, testCase.expectedSizes, sizes)
		})
	}
}

func TestRAMContentDefinedChunkerLongChunk(t *testing.T) {
	const windowSizeBytes = 8
	firstChunkSize := 3*64*1024 + 17
	data := make([]byte, firstChunkSize+windowSizeBytes+1)
	data[0] = 0xff
	data[firstChunkSize-1] = 0xff
	data[firstChunkSize] = 0xff

	chunker := cdc.NewRAMContentDefinedChunker(windowSizeBytes)
	chunks := chunkerChunks(t, chunker, data)
	require.Equal(t, []int{firstChunkSize, windowSizeBytes + 1}, []int{
		len(chunks[0]),
		len(chunks[1]),
	})
	require.Equal(t, data, bytes.Join(chunks, nil))
}

func TestRAMLContentDefinedChunkerMaximumLength(t *testing.T) {
	const windowSizeBytes = 20 * 1024
	maximumChunkSizeBytes := 4 * windowSizeBytes
	data := make([]byte, maximumChunkSizeBytes+windowSizeBytes+1)
	data[0] = 0xff
	data[maximumChunkSizeBytes] = 0xff

	chunker := cdc.NewRAMLContentDefinedChunker(windowSizeBytes)
	chunks := chunkerChunks(t, chunker, data)
	require.Equal(t, []int{maximumChunkSizeBytes, windowSizeBytes + 1}, []int{
		len(chunks[0]),
		len(chunks[1]),
	})
	require.Equal(t, data, bytes.Join(chunks, nil))
}

func FuzzRAMContentDefinedChunker(f *testing.F) {
	f.Add([]byte(nil), uint16(1))
	f.Add([]byte{1, 9, 2, 8, 9}, uint16(3))
	f.Add([]byte{0xff, 1, 2, 3, 0xff}, uint16(2))

	f.Fuzz(func(t *testing.T, data []byte, rawWindowSize uint16) {
		windowSizeBytes := int(rawWindowSize%4096) + 1
		chunker := cdc.NewRAMContentDefinedChunker(windowSizeBytes)
		expected := oracleChunks(data, func(remaining []byte) int {
			return ramChunkSize(remaining, windowSizeBytes)
		})
		require.Equal(t, expected, chunkerChunks(t, chunker, data))
	})
}

func FuzzRAMLContentDefinedChunker(f *testing.F) {
	f.Add([]byte(nil), uint16(1))
	f.Add([]byte{1, 9, 2, 8, 9}, uint16(3))
	f.Add(bytes.Repeat([]byte{0xff, 0}, 64), uint16(8))

	f.Fuzz(func(t *testing.T, data []byte, rawWindowSize uint16) {
		windowSizeBytes := int(rawWindowSize%4096) + 1
		chunker := cdc.NewRAMLContentDefinedChunker(windowSizeBytes)
		expected := oracleChunks(data, func(remaining []byte) int {
			return min(ramChunkSize(remaining, windowSizeBytes), 4*windowSizeBytes)
		})
		require.Equal(t, expected, chunkerChunks(t, chunker, data))
	})
}
