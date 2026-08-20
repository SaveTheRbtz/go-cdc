package cdc_test

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/buildbarn/go-cdc"
	"github.com/stretchr/testify/require"
)

func naiveSeqChunkSize(data []byte, normalSizeBytes int) int {
	sequenceLength := 5
	var minimumChunkSizeBytes, maximumChunkSizeBytes int
	var skipTrigger, skipSizeBytes int
	switch normalSizeBytes {
	case 4 * 1024:
		minimumChunkSizeBytes = 1024
		maximumChunkSizeBytes = 8192
		skipTrigger = 55
		skipSizeBytes = 256
	case 8 * 1024:
		minimumChunkSizeBytes = 4096
		maximumChunkSizeBytes = 16384
		skipTrigger = 50
		skipSizeBytes = 256
	case 16 * 1024:
		minimumChunkSizeBytes = 8192
		maximumChunkSizeBytes = 32768
		skipTrigger = 50
		skipSizeBytes = 512
	default:
		panic("unsupported oracle configuration")
	}

	end := min(len(data), maximumChunkSizeBytes)
	if end <= minimumChunkSizeBytes {
		return end
	}

	position := minimumChunkSizeBytes - sequenceLength + 1
	increasingLength := 1
	decreasingCount := 0
	for position < end {
		left := data[position-1]
		right := data[position]
		position++

		if right > left {
			increasingLength++
			if increasingLength == sequenceLength {
				return position
			}
		} else {
			increasingLength = 1
			if right < left {
				decreasingCount++
				if decreasingCount == skipTrigger {
					position += skipSizeBytes + 1
					decreasingCount = 0
				}
			}
		}
	}
	return end
}

func TestSeqContentDefinedChunkerValidation(t *testing.T) {
	for _, normalSizeBytes := range []int{-1, 0, 4095, 4097, 8191, 16385, 32768} {
		require.Panics(t, func() {
			cdc.NewSeqContentDefinedChunker(normalSizeBytes)
		})
	}

	for _, testCase := range []struct {
		normalSizeBytes  int
		maximumSizeBytes int
	}{
		{normalSizeBytes: 4 * 1024, maximumSizeBytes: 8 * 1024},
		{normalSizeBytes: 8 * 1024, maximumSizeBytes: 16 * 1024},
		{normalSizeBytes: 16 * 1024, maximumSizeBytes: 32 * 1024},
	} {
		chunker := cdc.NewSeqContentDefinedChunker(testCase.normalSizeBytes)
		require.False(t, chunker.SupportsDiscardUpToGuaranteedChunk())
		require.Equal(t, testCase.maximumSizeBytes, chunker.GetMaximumPeekSizeBytes())
		require.Panics(t, func() {
			chunker.DiscardUpToGuaranteedChunk(bufio.NewReader(bytes.NewReader(nil)))
		})
	}
}

func TestSeqContentDefinedChunkerBoundaries(t *testing.T) {
	const minimumChunkSizeBytes = 1024

	increasing := make([]byte, minimumChunkSizeBytes+1)
	copy(increasing[minimumChunkSizeBytes-5:], []byte{1, 2, 3, 4, 5})

	equality := make([]byte, minimumChunkSizeBytes+3)
	copy(equality[minimumChunkSizeBytes-5:], []byte{1, 2, 2, 3, 4, 5, 6})

	reset := make([]byte, minimumChunkSizeBytes+4)
	copy(reset[minimumChunkSizeBytes-5:], []byte{1, 2, 3, 1, 2, 3, 4, 5})

	testCases := map[string]struct {
		data          []byte
		expectedSizes []int
	}{
		"Empty": {},
		"ShortFinalChunk": {
			data:          make([]byte, minimumChunkSizeBytes-1),
			expectedSizes: []int{minimumChunkSizeBytes - 1},
		},
		"TerminalByteIsIncluded": {
			data:          increasing,
			expectedSizes: []int{minimumChunkSizeBytes, 1},
		},
		"EqualityBreaksStrictSequence": {
			data:          equality,
			expectedSizes: []int{minimumChunkSizeBytes + 2, 1},
		},
		"DecreaseResetsSequence": {
			data:          reset,
			expectedSizes: []int{minimumChunkSizeBytes + 3, 1},
		},
		"MaximumChunkSize": {
			data:          make([]byte, 8192+7),
			expectedSizes: []int{8192, 7},
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			chunks := chunkerChunks(t, cdc.NewSeqContentDefinedChunker(4*1024), testCase.data)
			var sizes []int
			for _, chunk := range chunks {
				sizes = append(sizes, len(chunk))
			}
			require.Equal(t, testCase.expectedSizes, sizes)
		})
	}
}

func TestSeqContentDefinedChunkerSkipsAfterOpposingSlopes(t *testing.T) {
	const minimumChunkSizeBytes = 4096
	expectedFirstChunkSizeBytes := minimumChunkSizeBytes + 307
	data := make([]byte, expectedFirstChunkSizeBytes+1)

	// The 50th decreasing comparison skips positions [min+46, min+302).
	data[minimumChunkSizeBytes-5] = 100
	for i := 0; i < 50; i++ {
		data[minimumChunkSizeBytes-4+i] = byte(99 - i)
	}
	copy(data[minimumChunkSizeBytes+60:], []byte{1, 2, 3, 4, 5, 6})
	data[minimumChunkSizeBytes+301] = 9
	copy(data[minimumChunkSizeBytes+302:], []byte{10, 11, 12, 13, 14})

	chunks := chunkerChunks(t, cdc.NewSeqContentDefinedChunker(8*1024), data)
	require.Equal(t, []int{expectedFirstChunkSizeBytes, 1}, []int{
		len(chunks[0]),
		len(chunks[1]),
	})
}

func TestSeqContentDefinedChunkerMatchesOracle(t *testing.T) {
	data := make([]byte, 512*1024)
	state := uint64(0x6a09e667f3bcc909)
	for i := range data {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		data[i] = byte(state)
	}

	for _, normalSizeBytes := range []int{4 * 1024, 8 * 1024, 16 * 1024} {
		expected := oracleChunks(data, func(remaining []byte) int {
			return naiveSeqChunkSize(remaining, normalSizeBytes)
		})
		actual := chunkerChunks(t, cdc.NewSeqContentDefinedChunker(normalSizeBytes), data)
		require.Equal(t, expected, actual)
	}
}

func FuzzSeqContentDefinedChunker(f *testing.F) {
	f.Add([]byte(nil), byte(0))
	f.Add(bytes.Repeat([]byte{1}, 40*1024), byte(1))
	f.Add(bytes.Repeat([]byte{3, 2, 1, 2, 3, 4, 5, 6}, 5*1024), byte(2))

	f.Fuzz(func(t *testing.T, data []byte, configuration byte) {
		normalSizeBytes := []int{4 * 1024, 8 * 1024, 16 * 1024}[configuration%3]
		expected := oracleChunks(data, func(remaining []byte) int {
			return naiveSeqChunkSize(remaining, normalSizeBytes)
		})
		actual := chunkerChunks(t, cdc.NewSeqContentDefinedChunker(normalSizeBytes), data)
		require.Equal(t, expected, actual)
	})
}
