package cdc_test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"math/rand"
	"testing"

	"github.com/buildbarn/go-cdc"
	"github.com/stretchr/testify/require"
)

func TestWideAsymmetricExtremumContentDefinedChunkerValidation(t *testing.T) {
	for _, regionSizeBytes := range []int{-1, 0, 3, 5, 16} {
		require.Panics(t, func() {
			cdc.NewWideAsymmetricExtremumContentDefinedChunker(regionSizeBytes, 1)
		})
	}
	for _, windowSizeBytes := range []int{-1, 0} {
		require.Panics(t, func() {
			cdc.NewWideAsymmetricExtremumContentDefinedChunker(8, windowSizeBytes)
		})
	}
	require.Panics(t, func() {
		cdc.NewWideAsymmetricExtremumContentDefinedChunker(8, math.MaxInt-7)
	})
	require.Equal(
		t,
		math.MaxInt,
		cdc.NewWideAsymmetricExtremumContentDefinedChunker(8, math.MaxInt-8).
			GetMaximumPeekSizeBytes(),
	)

	chunker := cdc.NewWideAsymmetricExtremumContentDefinedChunker(8, 1024)
	require.False(t, chunker.SupportsDiscardUpToGuaranteedChunk())
	require.Equal(t, 64*1024, chunker.GetMaximumPeekSizeBytes())
	require.Panics(t, func() {
		chunker.DiscardUpToGuaranteedChunk(bufio.NewReader(bytes.NewReader(nil)))
	})

	largeWindowChunker := cdc.NewWideAsymmetricExtremumContentDefinedChunker(8, 128*1024)
	require.Equal(t, 128*1024+8, largeWindowChunker.GetMaximumPeekSizeBytes())
}

func TestWideAsymmetricExtremumContentDefinedChunkerLittleEndian(t *testing.T) {
	data := []byte{0x01, 0x02, 0x01, 0x00, 0x00}
	chunker := cdc.NewWideAsymmetricExtremumContentDefinedChunker(2, 1)
	chunks := wideAsymmetricExtremumChunks(t, chunker, data)
	require.NotEmpty(t, chunks)
	require.Len(t, chunks[0], 2)
}

func TestWideAsymmetricExtremumContentDefinedChunkerOneByteMatchesAE(t *testing.T) {
	randomData := make([]byte, 3*64*1024+17)
	rand.New(rand.NewSource(11)).Read(randomData)
	repeatedData := bytes.Repeat([]byte("wide AE"), 32*1024)
	const refillWindowSizeBytes = 40 * 1024
	refillData := make([]byte, 3*refillWindowSizeBytes+2)
	refillData[refillWindowSizeBytes] = 1

	testCases := map[string]struct {
		windowSizeBytes int
		data            []byte
	}{
		"Random": {
			windowSizeBytes: 4096,
			data:            randomData,
		},
		"Repeated": {
			windowSizeBytes: 4096,
			data:            repeatedData,
		},
		"Refill": {
			windowSizeBytes: refillWindowSizeBytes,
			data:            refillData,
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			wideChunks := wideAsymmetricExtremumChunks(
				t,
				cdc.NewWideAsymmetricExtremumContentDefinedChunker(
					1,
					testCase.windowSizeBytes,
				),
				testCase.data,
			)
			aeChunks := wideAsymmetricExtremumChunks(
				t,
				cdc.NewAsymmetricExtremumContentDefinedChunker(
					testCase.windowSizeBytes,
				),
				testCase.data,
			)
			require.Equal(t, aeChunks, wideChunks)
		})
	}
}

func TestWideAsymmetricExtremumContentDefinedChunkerKeepsLeftmostTie(t *testing.T) {
	const windowSizeBytes = 3
	data := bytes.Repeat([]byte{0x5a}, 32)
	for _, regionSizeBytes := range []int{1, 2, 4, 8} {
		t.Run(fmt.Sprintf("RegionSize=%d", regionSizeBytes), func(t *testing.T) {
			chunker := cdc.NewWideAsymmetricExtremumContentDefinedChunker(
				regionSizeBytes,
				windowSizeBytes,
			)
			chunks := wideAsymmetricExtremumChunks(t, chunker, data)
			require.NotEmpty(t, chunks)
			require.Len(t, chunks[0], windowSizeBytes+1)
		})
	}
}

func TestWideAsymmetricExtremumContentDefinedChunkerBoundaryTails(t *testing.T) {
	const windowSizeBytes = 17
	for _, regionSizeBytes := range []int{1, 2, 4, 8} {
		for _, sizeBytes := range []int{
			0,
			1,
			regionSizeBytes - 1,
			regionSizeBytes,
			windowSizeBytes,
			windowSizeBytes + 1,
			windowSizeBytes + regionSizeBytes - 1,
			windowSizeBytes + regionSizeBytes,
			2*windowSizeBytes + regionSizeBytes,
		} {
			t.Run(fmt.Sprintf(
				"RegionSize=%d/Size=%d",
				regionSizeBytes,
				sizeBytes,
			), func(t *testing.T) {
				data := make([]byte, sizeBytes)
				rand.New(rand.NewSource(int64(100*regionSizeBytes + sizeBytes))).Read(data)
				checkWideAsymmetricExtremumAgainstOracle(
					t,
					data,
					regionSizeBytes,
					windowSizeBytes,
				)
			})
		}
	}
}

func TestWideAsymmetricExtremumContentDefinedChunkerMatchesOracle(t *testing.T) {
	const dataSizeBytes = 96 * 1024
	randomData := make([]byte, dataSizeBytes)
	rand.New(rand.NewSource(1)).Read(randomData)
	rampData := make([]byte, dataSizeBytes)
	for i := range rampData {
		rampData[i] = byte(i)
	}
	repeatedData := bytes.Repeat([]byte("wide asymmetric extremum"), dataSizeBytes/24+1)
	repeatedData = repeatedData[:dataSizeBytes]

	inputs := map[string][]byte{
		"Random":   randomData,
		"Zeros":    make([]byte, dataSizeBytes),
		"Ramp":     rampData,
		"Repeated": repeatedData,
	}
	for _, regionSizeBytes := range []int{1, 2, 4, 8} {
		for _, windowSizeBytes := range []int{1, 7, 64, 4096} {
			for name, data := range inputs {
				t.Run(fmt.Sprintf(
					"%s/RegionSize=%d/Window=%d",
					name,
					regionSizeBytes,
					windowSizeBytes,
				), func(t *testing.T) {
					checkWideAsymmetricExtremumAgainstOracle(
						t,
						data,
						regionSizeBytes,
						windowSizeBytes,
					)
				})
			}
		}
	}
}

func TestWideAsymmetricExtremumContentDefinedChunkerRefill(t *testing.T) {
	const windowSizeBytes = 40 * 1024
	for _, regionSizeBytes := range []int{1, 2, 4, 8} {
		t.Run(fmt.Sprintf("RegionSize=%d", regionSizeBytes), func(t *testing.T) {
			candidateOffset := windowSizeBytes - regionSizeBytes + 1
			firstChunkSize := candidateOffset + windowSizeBytes + 1
			data := make([]byte, firstChunkSize+windowSizeBytes+regionSizeBytes)
			data[windowSizeBytes] = 1

			chunker := cdc.NewWideAsymmetricExtremumContentDefinedChunker(
				regionSizeBytes,
				windowSizeBytes,
			)
			chunks := wideAsymmetricExtremumChunks(t, chunker, data)
			require.NotEmpty(t, chunks)
			require.Len(t, chunks[0], firstChunkSize)
			require.Equal(t, data, bytes.Join(chunks, nil))
			checkWideAsymmetricExtremumAgainstOracle(
				t,
				data,
				regionSizeBytes,
				windowSizeBytes,
			)
		})
	}
}

func FuzzWideAsymmetricExtremumContentDefinedChunker(f *testing.F) {
	f.Add([]byte(nil), uint8(0), uint16(0))
	f.Add(bytes.Repeat([]byte{0xff}, 4096), uint8(3), uint16(63))
	f.Add([]byte("overlapping little-endian values"), uint8(2), uint16(17))

	regionSizes := [...]int{1, 2, 4, 8}
	f.Fuzz(func(t *testing.T, data []byte, regionSeed uint8, windowSeed uint16) {
		regionSizeBytes := regionSizes[regionSeed%uint8(len(regionSizes))]
		windowSizeBytes := int(windowSeed%4096) + 1
		if len(data) > 128*1024 {
			data = data[:128*1024]
		}
		checkWideAsymmetricExtremumAgainstOracle(
			t,
			data,
			regionSizeBytes,
			windowSizeBytes,
		)
	})
}

func BenchmarkWideAsymmetricExtremumContentDefinedChunker(b *testing.B) {
	const (
		windowSizeBytes = 8 * 1024
		dataSizeBytes   = 64 * 1024 * 1024
	)
	data := make([]byte, dataSizeBytes)
	rand.New(rand.NewSource(1)).Read(data)

	for _, regionSizeBytes := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("RegionSize=%d", regionSizeBytes), func(b *testing.B) {
			chunker := cdc.NewWideAsymmetricExtremumContentDefinedChunker(
				regionSizeBytes,
				windowSizeBytes,
			)
			reader := bytes.NewReader(data)
			bufferedReader := bufio.NewReaderSize(
				reader,
				chunker.GetMaximumPeekSizeBytes(),
			)

			b.SetBytes(dataSizeBytes)
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

// referenceWideAsymmetricExtremumChunkSize is deliberately naive. It decodes
// every complete overlapping value byte by byte and implements the AE rule
// literally, independently of the production decoder and refill logic.
func referenceWideAsymmetricExtremumChunkSize(
	data []byte,
	regionSizeBytes, windowSizeBytes int,
) int {
	if len(data) < regionSizeBytes {
		return len(data)
	}

	maximum := referenceLittleEndianValue(data, regionSizeBytes)
	candidate := 0
	for position := 1; position+regionSizeBytes <= len(data); position++ {
		value := referenceLittleEndianValue(data[position:], regionSizeBytes)
		if value > maximum {
			maximum = value
			candidate = position
		} else if position-candidate == windowSizeBytes {
			return position + 1
		}
	}
	return len(data)
}

func referenceLittleEndianValue(data []byte, regionSizeBytes int) uint64 {
	var value uint64
	for i := 0; i < regionSizeBytes; i++ {
		value |= uint64(data[i]) << (8 * i)
	}
	return value
}

func checkWideAsymmetricExtremumAgainstOracle(
	t *testing.T,
	data []byte,
	regionSizeBytes, windowSizeBytes int,
) {
	t.Helper()
	chunker := cdc.NewWideAsymmetricExtremumContentDefinedChunker(
		regionSizeBytes,
		windowSizeBytes,
	)
	actual := wideAsymmetricExtremumChunks(t, chunker, data)

	var expected [][]byte
	remaining := data
	for len(remaining) > 0 {
		sizeBytes := referenceWideAsymmetricExtremumChunkSize(
			remaining,
			regionSizeBytes,
			windowSizeBytes,
		)
		expected = append(expected, bytes.Clone(remaining[:sizeBytes]))
		remaining = remaining[sizeBytes:]
	}
	require.Equal(t, expected, actual)
}

func wideAsymmetricExtremumChunks(
	t *testing.T,
	chunker cdc.ContentDefinedChunker,
	data []byte,
) [][]byte {
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
