package cdc_test

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/buildbarn/go-cdc"
	"github.com/stretchr/testify/require"
)

func asymmetricExtremumChunkSize(data []byte, windowSizeBytes int) int {
	if len(data) == 0 {
		return 0
	}

	maximum := data[0]
	candidate := 0
	for i := 1; i < len(data); i++ {
		if data[i] > maximum {
			maximum = data[i]
			candidate = i
		} else if i-candidate == windowSizeBytes {
			return i + 1
		}
	}
	return len(data)
}

func oracleChunks(data []byte, chunkSize func([]byte) int) [][]byte {
	var chunks [][]byte
	for len(data) > 0 {
		sizeBytes := chunkSize(data)
		chunks = append(chunks, bytes.Clone(data[:sizeBytes]))
		data = data[sizeBytes:]
	}
	return chunks
}

func chunkerChunks(t *testing.T, chunker cdc.ContentDefinedChunker, data []byte) [][]byte {
	t.Helper()

	chunkReader := chunker.NewChunkReader(bufio.NewReaderSize(
		bytes.NewReader(data),
		chunker.GetMaximumPeekSizeBytes(),
	))
	var chunks [][]byte
	for {
		chunk, err := chunkReader.ReadNextChunk()
		if err == io.EOF {
			return chunks
		}
		require.NoError(t, err)
		require.NotEmpty(t, chunk)
		chunks = append(chunks, bytes.Clone(chunk))
	}
}

func TestAsymmetricExtremumContentDefinedChunkerValidation(t *testing.T) {
	for _, windowSizeBytes := range []int{-1, 0, math.MaxInt} {
		require.Panics(t, func() {
			cdc.NewAsymmetricExtremumContentDefinedChunker(windowSizeBytes)
		})
	}

	chunker := cdc.NewAsymmetricExtremumContentDefinedChunker(1024)
	require.False(t, chunker.SupportsDiscardUpToGuaranteedChunk())
	require.Equal(t, 64*1024, chunker.GetMaximumPeekSizeBytes())
	require.Panics(t, func() {
		chunker.DiscardUpToGuaranteedChunk(bufio.NewReader(bytes.NewReader(nil)))
	})

	largeWindowChunker := cdc.NewAsymmetricExtremumContentDefinedChunker(128 * 1024)
	require.Equal(t, 128*1024+1, largeWindowChunker.GetMaximumPeekSizeBytes())
}

func TestAsymmetricExtremumContentDefinedChunkerBoundaries(t *testing.T) {
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
		"FirstByteSurvives": {
			windowSizeBytes: 3,
			data:            []byte{9, 1, 2, 3, 8},
			expectedSizes:   []int{4, 1},
		},
		"EqualityDoesNotReset": {
			windowSizeBytes: 3,
			data:            []byte{5, 5, 5, 5, 4},
			expectedSizes:   []int{4, 1},
		},
		"NewMaximumAtDeadline": {
			windowSizeBytes: 3,
			data:            []byte{5, 1, 2, 6, 1, 2, 3, 9},
			expectedSizes:   []int{7, 1},
		},
		"MaximumByte": {
			windowSizeBytes: 3,
			data:            []byte{1, 0xff, 7, 8, 9, 4},
			expectedSizes:   []int{5, 1},
		},
		"MinimumWindow": {
			windowSizeBytes: 1,
			data:            []byte{2, 1, 4, 3},
			expectedSizes:   []int{2, 2},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			chunker := cdc.NewAsymmetricExtremumContentDefinedChunker(testCase.windowSizeBytes)
			chunks := chunkerChunks(t, chunker, testCase.data)
			var sizes []int
			for _, chunk := range chunks {
				sizes = append(sizes, len(chunk))
			}
			require.Equal(t, testCase.expectedSizes, sizes)
		})
	}
}

func TestAsymmetricExtremumContentDefinedChunkerLongChunk(t *testing.T) {
	const (
		windowSizeBytes = 1024
		recordCount     = 70
	)
	firstChunkSize := (recordCount+1)*windowSizeBytes + 1
	data := make([]byte, firstChunkSize+windowSizeBytes+1)
	for record := 1; record <= recordCount; record++ {
		data[record*windowSizeBytes] = byte(record)
	}
	data[firstChunkSize] = 0xff

	chunker := cdc.NewAsymmetricExtremumContentDefinedChunker(windowSizeBytes)
	chunks := chunkerChunks(t, chunker, data)
	require.Equal(t, []int{firstChunkSize, windowSizeBytes + 1}, []int{
		len(chunks[0]),
		len(chunks[1]),
	})
	require.Equal(t, data, bytes.Join(chunks, nil))
}

func FuzzAsymmetricExtremumContentDefinedChunker(f *testing.F) {
	f.Add([]byte(nil), uint16(1))
	f.Add([]byte{5, 5, 5, 5}, uint16(3))
	f.Add([]byte{1, 0xff, 1, 2, 3, 4}, uint16(3))

	f.Fuzz(func(t *testing.T, data []byte, rawWindowSize uint16) {
		windowSizeBytes := int(rawWindowSize%4096) + 1
		chunker := cdc.NewAsymmetricExtremumContentDefinedChunker(windowSizeBytes)
		expected := oracleChunks(data, func(remaining []byte) int {
			return asymmetricExtremumChunkSize(remaining, windowSizeBytes)
		})
		require.Equal(t, expected, chunkerChunks(t, chunker, data))
	})
}
