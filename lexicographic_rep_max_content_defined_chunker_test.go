package cdc

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLexicographicRepMaxContentDefinedChunkerInvalidParameters(t *testing.T) {
	t.Run("NonPositiveWindow", func(t *testing.T) {
		require.Panics(t, func() {
			NewLexicographicRepMaxContentDefinedChunker(0, 1, 0)
		})
		require.Panics(t, func() {
			NewLexicographicRepMaxContentDefinedChunker(-1, 1, 0)
		})
	})
	t.Run("WindowLargerThanMinimum", func(t *testing.T) {
		require.Panics(t, func() {
			NewLexicographicRepMaxContentDefinedChunker(65, 64, 0)
		})
	})
	t.Run("NegativeHorizon", func(t *testing.T) {
		require.Panics(t, func() {
			NewLexicographicRepMaxContentDefinedChunker(1, 1, -1)
		})
	})
	t.Run("PeekSizeOverflow", func(t *testing.T) {
		require.Panics(t, func() {
			NewLexicographicRepMaxContentDefinedChunker(1, math.MaxInt/2+1, 0)
		})
	})
}

func TestLexicographicRepMaxContentDefinedChunkerProperties(t *testing.T) {
	const (
		windowSizeBytes  = 17
		minSizeBytes     = 64
		horizonSizeBytes = 8 * minSizeBytes
	)
	chunker := NewLexicographicRepMaxContentDefinedChunker(
		windowSizeBytes,
		minSizeBytes,
		horizonSizeBytes,
	)
	require.False(t, chunker.SupportsDiscardUpToGuaranteedChunk())
	require.Equal(t, 2*minSizeBytes+horizonSizeBytes, chunker.GetMaximumPeekSizeBytes())
	require.Panics(t, func() {
		chunker.DiscardUpToGuaranteedChunk(bufio.NewReader(bytes.NewReader(nil)))
	})
}

func TestLexicographicRepMaxContentDefinedChunkerByteOrder(t *testing.T) {
	data := make([]byte, 11)
	copy(data[2:], []byte{0x10, 0xff, 0x20, 0x00, 0x21})
	chunker := NewLexicographicRepMaxContentDefinedChunker(2, 4, 3)
	chunks := readAllLexicographicRepMaxChunks(t, chunker, data)
	require.Len(t, chunks, 2)
	require.Len(t, chunks[0], 5)
}

func TestLexicographicRepMaxContentDefinedChunkerKeepsLeftmostTie(t *testing.T) {
	data := make([]byte, 11)
	copy(data[2:], []byte{0xff, 0x00, 0xff, 0x00, 0x00})
	chunker := NewLexicographicRepMaxContentDefinedChunker(2, 4, 3)
	chunks := readAllLexicographicRepMaxChunks(t, chunker, data)
	require.Len(t, chunks, 2)
	require.Len(t, chunks[0], 4)
}

func TestLexicographicRepMaxContentDefinedChunkerComparesBeyondPrefix(t *testing.T) {
	data := make([]byte, 21)
	for i := 1; i < 10; i++ {
		data[i] = 0x80
	}
	data[10] = 0x81
	chunker := NewLexicographicRepMaxContentDefinedChunker(9, 10, 1)
	chunks := readAllLexicographicRepMaxChunks(t, chunker, data)
	require.Len(t, chunks, 2)
	require.Len(t, chunks[0], 11)
}

func TestLexicographicRepMaxContentDefinedChunkerBoundaryLengths(t *testing.T) {
	const (
		windowSizeBytes  = 9
		minSizeBytes     = 17
		horizonSizeBytes = 51
	)
	for _, sizeBytes := range []int{
		0,
		1,
		windowSizeBytes - 1,
		windowSizeBytes,
		minSizeBytes - 1,
		minSizeBytes,
		2*minSizeBytes - 1,
		2 * minSizeBytes,
		2*minSizeBytes + 1,
		2*minSizeBytes + horizonSizeBytes - 1,
		2*minSizeBytes + horizonSizeBytes,
		2*minSizeBytes + horizonSizeBytes + 1,
	} {
		t.Run(fmt.Sprintf("Size=%d", sizeBytes), func(t *testing.T) {
			data := make([]byte, sizeBytes)
			rand.New(rand.NewSource(int64(sizeBytes))).Read(data)
			checkLexicographicRepMaxAgainstAlgorithm8(
				t,
				data,
				windowSizeBytes,
				minSizeBytes,
				horizonSizeBytes,
			)
		})
	}
}

func TestLexicographicRepMaxContentDefinedChunkerMatchesAlgorithm8(t *testing.T) {
	const dataSizeBytes = 32 * 1024

	randomData := make([]byte, dataSizeBytes)
	rand.New(rand.NewSource(1)).Read(randomData)
	zeroData := make([]byte, dataSizeBytes)
	rampData := make([]byte, dataSizeBytes)
	for i := range rampData {
		rampData[i] = byte(i)
	}
	repeatedData := bytes.Repeat([]byte("lexicographic RepMax"), dataSizeBytes/20+1)
	repeatedData = repeatedData[:dataSizeBytes]

	inputs := map[string][]byte{
		"Random":   randomData,
		"Zeros":    zeroData,
		"Ramp":     rampData,
		"Repeated": repeatedData,
	}
	for _, windowSizeBytes := range []int{1, 2, 7, 8, 9, 31, 64, 257} {
		minSizeBytes := max(windowSizeBytes, 257)
		for _, horizonSizeBytes := range []int{
			0,
			minSizeBytes - 1,
			2 * (minSizeBytes - 1),
			8 * minSizeBytes,
		} {
			for name, data := range inputs {
				t.Run(fmt.Sprintf(
					"%s/Window=%d/Horizon=%d",
					name,
					windowSizeBytes,
					horizonSizeBytes,
				), func(t *testing.T) {
					checkLexicographicRepMaxAgainstAlgorithm8(
						t,
						data,
						windowSizeBytes,
						minSizeBytes,
						horizonSizeBytes,
					)
				})
			}
		}
	}
}

func FuzzLexicographicRepMaxContentDefinedChunker(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint16(0), []byte(nil))
	f.Add(uint8(7), uint8(63), uint16(511), bytes.Repeat([]byte{0xff}, 4096))
	f.Add(uint8(8), uint8(19), uint16(997), []byte("overlapping lexicographic windows"))

	f.Fuzz(func(
		t *testing.T,
		windowSeed, minimumExtra uint8,
		horizonSeed uint16,
		data []byte,
	) {
		windowSizeBytes := int(windowSeed%64) + 1
		minSizeBytes := windowSizeBytes + int(minimumExtra)
		horizonSizeBytes := int(horizonSeed) % (4*minSizeBytes + 1)
		if len(data) > 128*1024 {
			data = data[:128*1024]
		}
		checkLexicographicRepMaxAgainstAlgorithm8(
			t,
			data,
			windowSizeBytes,
			minSizeBytes,
			horizonSizeBytes,
		)
	})
}

func BenchmarkLexicographicRepMaxContentDefinedChunker(b *testing.B) {
	const (
		minSizeBytes     = 256 * 1024
		horizonSizeBytes = 8 * minSizeBytes
		sizeBytes        = 64 * 1024 * 1024
	)
	data := make([]byte, sizeBytes)
	rand.New(rand.NewSource(1)).Read(data)

	for _, windowSizeBytes := range []int{1, 8, 64, 256} {
		b.Run(fmt.Sprintf("Window=%d", windowSizeBytes), func(b *testing.B) {
			chunker := NewLexicographicRepMaxContentDefinedChunker(
				windowSizeBytes,
				minSizeBytes,
				horizonSizeBytes,
			)
			reader := bytes.NewReader(data)
			bufferedReader := bufio.NewReaderSize(reader, chunker.GetMaximumPeekSizeBytes())

			b.SetBytes(sizeBytes)
			for b.Loop() {
				reader.Reset(data)
				bufferedReader.Reset(reader)
				chunkReader := chunker.NewChunkReader(bufferedReader)
				for {
					if _, err := chunkReader.ReadNextChunk(); err != nil {
						if err == io.EOF {
							break
						}
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// referenceLexicographicRepMaxFirstChunk is a literal implementation of
// RepMaxCDC Algorithm 8. It deliberately compares complete windows and does
// not share the optimized frontier or prefix scanner with the implementation.
func referenceLexicographicRepMaxFirstChunk(
	data []byte,
	windowSizeBytes, minSizeBytes, horizonSizeBytes int,
) int {
	n := min(len(data), 2*minSizeBytes+horizonSizeBytes)
	for n >= 2*minSizeBytes {
		maximumCut := minSizeBytes
		maximumWindow := data[maximumCut-windowSizeBytes : maximumCut]
		for cut := maximumCut + 1; cut <= n-minSizeBytes; cut++ {
			window := data[cut-windowSizeBytes : cut]
			if bytes.Compare(maximumWindow, window) < 0 {
				maximumCut = cut
				maximumWindow = window
			}
		}
		n = maximumCut
	}
	return n
}

func checkLexicographicRepMaxAgainstAlgorithm8(
	t *testing.T,
	data []byte,
	windowSizeBytes, minSizeBytes, horizonSizeBytes int,
) {
	t.Helper()
	chunker := NewLexicographicRepMaxContentDefinedChunker(
		windowSizeBytes,
		minSizeBytes,
		horizonSizeBytes,
	)
	chunks := readAllLexicographicRepMaxChunks(t, chunker, data)

	offset := 0
	for _, chunk := range chunks {
		expectedSize := referenceLexicographicRepMaxFirstChunk(
			data[offset:],
			windowSizeBytes,
			minSizeBytes,
			horizonSizeBytes,
		)
		require.Equal(t, expectedSize, len(chunk))
		require.Equal(t, data[offset:offset+expectedSize], chunk)
		offset += expectedSize
	}
	require.Equal(t, len(data), offset)
}

func readAllLexicographicRepMaxChunks(
	t *testing.T,
	chunker ContentDefinedChunker,
	data []byte,
) [][]byte {
	t.Helper()
	reader := bytes.NewReader(data)
	bufferedReader := bufio.NewReaderSize(reader, chunker.GetMaximumPeekSizeBytes())
	chunkReader := chunker.NewChunkReader(bufferedReader)

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
