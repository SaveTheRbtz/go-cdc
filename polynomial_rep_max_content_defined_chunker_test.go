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

func TestPolynomialHash(t *testing.T) {
	zeros := make([]byte, polynomialHashWindowSizeBytes)
	require.Equal(t, uint64(0xc7d350f3aa55b5c0), computePolynomialHash(zeros))

	ramp := make([]byte, 256)
	for i := range ramp {
		ramp[i] = byte(i)
	}
	require.Equal(t, uint64(0xa185503c0c0f2ba0), computePolynomialHash(ramp[:64]))
	require.Equal(t, uint64(0x6958a12fb664e160), computePolynomialHash(ramp[1:65]))

	hash := computePolynomialHash(ramp[:polynomialHashWindowSizeBytes])
	for end := polynomialHashWindowSizeBytes + 1; end <= len(ramp); end++ {
		hash = rollPolynomialHash(
			hash,
			ramp[end-polynomialHashWindowSizeBytes-1],
			ramp[end-1],
		)
		require.Equal(
			t,
			computePolynomialHash(ramp[end-polynomialHashWindowSizeBytes:end]),
			hash,
		)
	}
}

func TestPolynomialHashRemovalFactor(t *testing.T) {
	factor := uint64(1)
	for i := 0; i < polynomialHashWindowSizeBytes; i++ {
		factor *= polynomialHashBase
	}
	require.Equal(t, polynomialHashRemovalFactor, factor)
}

func TestNewPolyRepMaxContentDefinedChunkerInvalidParameters(t *testing.T) {
	t.Run("MinimumSizeTooSmall", func(t *testing.T) {
		require.Panics(t, func() {
			NewPolyRepMaxContentDefinedChunker(polynomialHashWindowSizeBytes-1, 0)
		})
	})
	t.Run("NegativeHorizon", func(t *testing.T) {
		require.Panics(t, func() {
			NewPolyRepMaxContentDefinedChunker(polynomialHashWindowSizeBytes, -1)
		})
	})
	t.Run("PeekSizeOverflow", func(t *testing.T) {
		require.Panics(t, func() {
			NewPolyRepMaxContentDefinedChunker(math.MaxInt/2+1, 0)
		})
	})
}

func TestPolynomialRepMaxContentDefinedChunker(t *testing.T) {
	randomData := make([]byte, 16*1024)
	_, err := rand.New(rand.NewSource(1)).Read(randomData)
	require.NoError(t, err)

	rampData := make([]byte, len(randomData))
	for i := range rampData {
		rampData[i] = byte(i)
	}

	inputs := map[string][]byte{
		"Empty":    nil,
		"Short":    bytes.Repeat([]byte{0x5a}, 31),
		"Constant": bytes.Repeat([]byte{0x5a}, len(randomData)),
		"Ramp":     rampData,
		"Random":   randomData,
	}

	for _, minSizeBytes := range []int{64, 257} {
		for _, horizonSizeBytes := range []int{
			0,
			minSizeBytes / 2,
			minSizeBytes - 1,
			minSizeBytes,
			2 * (minSizeBytes - 1),
			8 * minSizeBytes,
		} {
			for inputName, data := range inputs {
				t.Run(fmt.Sprintf("Min=%d/Horizon=%d/%s", minSizeBytes, horizonSizeBytes, inputName), func(t *testing.T) {
					testPolynomialRepMaxChunks(t, data, minSizeBytes, horizonSizeBytes)
				})
			}
		}
	}
}

func TestPolynomialRepMaxContentDefinedChunkerBoundaryLengths(t *testing.T) {
	const (
		minSizeBytes     = 64
		horizonSizeBytes = 64
	)
	peekSizeBytes := 2*minSizeBytes + horizonSizeBytes
	data := make([]byte, peekSizeBytes+1)
	_, err := rand.New(rand.NewSource(2)).Read(data)
	require.NoError(t, err)

	for _, sizeBytes := range []int{
		minSizeBytes - 1,
		minSizeBytes,
		minSizeBytes + 1,
		2*minSizeBytes - 1,
		2 * minSizeBytes,
		2*minSizeBytes + 1,
		peekSizeBytes - 1,
		peekSizeBytes,
		peekSizeBytes + 1,
	} {
		t.Run(fmt.Sprintf("Size=%d", sizeBytes), func(t *testing.T) {
			testPolynomialRepMaxChunks(t, data[:sizeBytes], minSizeBytes, horizonSizeBytes)
		})
	}
}

func TestPolynomialRepMaxContentDefinedChunkerLeftmostTies(t *testing.T) {
	const minSizeBytes = 64
	data := bytes.Repeat([]byte{0x5a}, 10*minSizeBytes)
	chunker := NewPolyRepMaxContentDefinedChunker(minSizeBytes, 8*minSizeBytes)
	chunkReader := chunker.NewChunkReader(bufio.NewReaderSize(
		bytes.NewReader(data),
		chunker.GetMaximumPeekSizeBytes(),
	))

	for offset := 0; offset < len(data); offset += minSizeBytes {
		chunk, err := chunkReader.ReadNextChunk()
		require.NoError(t, err)
		require.Len(t, chunk, minSizeBytes)
	}
	_, err := chunkReader.ReadNextChunk()
	require.ErrorIs(t, err, io.EOF)
}

type polyOffsetTrackingPeeker struct {
	Peeker
	offset int
}

func (p *polyOffsetTrackingPeeker) Discard(n int) (int, error) {
	discarded, err := p.Peeker.Discard(n)
	p.offset += discarded
	return discarded, err
}

func TestPolyRepMaxContentDefinedChunkerDiscardUpToGuaranteedChunk(t *testing.T) {
	const (
		minSizeBytes     = 2 * 1024
		horizonSizeBytes = 8 * minSizeBytes
	)
	chunker := NewPolyRepMaxContentDefinedChunker(minSizeBytes, horizonSizeBytes)
	require.True(t, chunker.SupportsDiscardUpToGuaranteedChunk())

	t.Run("Empty", func(t *testing.T) {
		peeker := bufio.NewReader(bytes.NewReader(nil))
		require.ErrorIs(t, chunker.DiscardUpToGuaranteedChunk(peeker), io.EOF)
	})

	t.Run("Constant", func(t *testing.T) {
		peeker := bufio.NewReader(bytes.NewReader(make([]byte, 1024*1024)))
		require.ErrorIs(t, chunker.DiscardUpToGuaranteedChunk(peeker), io.EOF)
	})

	t.Run("Random", func(t *testing.T) {
		random := rand.New(rand.NewSource(3))
		for i := 0; i < 1000; i++ {
			seed := random.Int63()
			peeker := polyOffsetTrackingPeeker{
				Peeker: bufio.NewReaderSize(rand.New(rand.NewSource(seed)), 64*1024),
			}
			_, err := peeker.Discard(random.Intn(horizonSizeBytes))
			require.NoError(t, err)
			require.NoError(t, chunker.DiscardUpToGuaranteedChunk(&peeker))

			chunkReader := chunker.NewChunkReader(
				bufio.NewReaderSize(rand.New(rand.NewSource(seed)), 64*1024),
			)
			for remainingBytes := peeker.offset; remainingBytes > 0; {
				chunk, err := chunkReader.ReadNextChunk()
				require.NoError(t, err)
				require.GreaterOrEqual(t, remainingBytes, len(chunk))
				remainingBytes -= len(chunk)
			}
		}
	})
}

func TestPolyRepMaxContentDefinedChunkerSynchronizationThreshold(t *testing.T) {
	const minSizeBytes = polynomialHashWindowSizeBytes

	unsupported := NewPolyRepMaxContentDefinedChunker(minSizeBytes, 2*(minSizeBytes-1)-1)
	require.False(t, unsupported.SupportsDiscardUpToGuaranteedChunk())
	require.Panics(t, func() {
		unsupported.DiscardUpToGuaranteedChunk(bufio.NewReader(bytes.NewReader(nil)))
	})

	supported := NewPolyRepMaxContentDefinedChunker(minSizeBytes, 2*(minSizeBytes-1))
	require.True(t, supported.SupportsDiscardUpToGuaranteedChunk())

	suffix := make([]byte, 64*1024)
	_, err := rand.New(rand.NewSource(4)).Read(suffix)
	require.NoError(t, err)
	peeker := polyOffsetTrackingPeeker{
		Peeker: bufio.NewReader(bytes.NewReader(suffix)),
	}
	require.NoError(t, supported.DiscardUpToGuaranteedChunk(&peeker))

	for _, prefix := range [][]byte{
		bytes.Repeat([]byte{0x00}, 4*1024),
		bytes.Repeat([]byte{0xff}, 4*1024),
		append(bytes.Repeat([]byte{0x5a}, 4*1024-1), 0xa5),
	} {
		data := append(append([]byte(nil), prefix...), suffix...)
		chunkReader := supported.NewChunkReader(bufio.NewReaderSize(
			bytes.NewReader(data),
			supported.GetMaximumPeekSizeBytes(),
		))
		targetOffset := len(prefix) + peeker.offset
		offset := 0
		for offset < targetOffset {
			chunk, err := chunkReader.ReadNextChunk()
			require.NoError(t, err)
			offset += len(chunk)
		}
		require.Equal(t, targetOffset, offset)
	}
}

func testPolynomialRepMaxChunks(t *testing.T, data []byte, minSizeBytes, horizonSizeBytes int) {
	t.Helper()

	chunker := NewPolyRepMaxContentDefinedChunker(minSizeBytes, horizonSizeBytes)
	require.Equal(t, 2*minSizeBytes+horizonSizeBytes, chunker.GetMaximumPeekSizeBytes())
	chunkReader := chunker.NewChunkReader(bufio.NewReaderSize(
		bytes.NewReader(data),
		chunker.GetMaximumPeekSizeBytes(),
	))

	for offset := 0; offset < len(data); {
		expectedSizeBytes := referencePolynomialRepMaxChunkLength(
			data[offset:],
			minSizeBytes,
			horizonSizeBytes,
		)
		chunk, err := chunkReader.ReadNextChunk()
		require.NoError(t, err)
		require.Equal(t, data[offset:offset+expectedSizeBytes], chunk)
		if len(data) >= minSizeBytes {
			require.GreaterOrEqual(t, len(chunk), minSizeBytes)
		}
		require.Less(t, len(chunk), 2*minSizeBytes)
		offset += len(chunk)
	}

	_, err := chunkReader.ReadNextChunk()
	require.ErrorIs(t, err, io.EOF)
}

// referencePolynomialRepMaxChunkLength is a literal implementation of
// Algorithm 8. Unlike the production implementation, it recomputes the
// polynomial hash independently for every eligible cutting point.
func referencePolynomialRepMaxChunkLength(data []byte, minSizeBytes, horizonSizeBytes int) int {
	n := min(len(data), 2*minSizeBytes+horizonSizeBytes)
	for n >= 2*minSizeBytes {
		bestCut := minSizeBytes
		bestHash := computePolynomialHash(data[bestCut-polynomialHashWindowSizeBytes : bestCut])
		for cut := bestCut + 1; cut <= n-minSizeBytes; cut++ {
			hash := computePolynomialHash(data[cut-polynomialHashWindowSizeBytes : cut])
			if bestHash < hash {
				bestHash = hash
				bestCut = cut
			}
		}
		n = bestCut
	}
	return n
}

func FuzzPolynomialRepMaxContentDefinedChunker(f *testing.F) {
	f.Add(uint16(0), uint16(0), []byte(nil))
	f.Add(uint16(1), uint16(1), bytes.Repeat([]byte{0x5a}, 1024))
	f.Add(uint16(193), uint16(math.MaxUint16), []byte("polynomial RepMaxCDC"))

	f.Fuzz(func(t *testing.T, minSeed, horizonSeed uint16, data []byte) {
		minSizeBytes := polynomialHashWindowSizeBytes + int(minSeed)%193
		horizonSizeBytes := int(horizonSeed) % (8*minSizeBytes + 1)
		if len(data) > 64*1024 {
			data = data[:64*1024]
		}
		testPolynomialRepMaxChunks(t, data, minSizeBytes, horizonSizeBytes)
	})
}
