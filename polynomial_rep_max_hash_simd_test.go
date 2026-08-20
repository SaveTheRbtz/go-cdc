//go:build !amd64 && goexperiment.simd

package cdc

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanPolynomialRepMaxRecordMaximaSIMD(t *testing.T) {
	random := rand.New(rand.NewSource(0x1fc635d4))
	lengths := []int{
		0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17,
		31, 32, 33, 63, 64, 65, 255, 256, 257,
		4095, 4096, 4097, 65535, 65536, 65537,
	}
	for range 100 {
		lengths = append(lengths, random.Intn(128<<10))
	}

	for _, length := range lengths {
		data := make([]byte, polynomialHashWindowSizeBytes+length)
		_, err := random.Read(data)
		require.NoError(t, err)

		for _, maximumHash := range []uint64{
			0,
			random.Uint64(),
			math.MaxUint64,
		} {
			currentHash := random.Uint64()
			baseOffset := random.Intn(4096)
			reference := repMaxFrontier{
				candidateOffsets: []int{3, 7},
				currentHash:      currentHash,
				maximumHash:      maximumHash,
			}
			portable := repMaxFrontier{
				candidateOffsets: []int{3, 7},
				currentHash:      currentHash,
				maximumHash:      maximumHash,
			}

			scanPolynomialRepMaxRecordMaximaNaive(
				data,
				baseOffset,
				&reference,
			)
			scanPolynomialRepMaxRecordMaxima(
				data,
				baseOffset,
				&portable,
			)
			require.Equal(t, reference, portable, "incoming length %d", length)
		}
	}
}

func TestScanPolynomialRepMaxRecordMaximaSIMDEqualHashes(t *testing.T) {
	data := make([]byte, polynomialHashWindowSizeBytes+257)
	initialHash := computePolynomialHash(data[:polynomialHashWindowSizeBytes])
	frontier := repMaxFrontier{
		candidateOffsets: []int{0},
		currentHash:      initialHash,
		maximumHash:      initialHash,
	}

	scanPolynomialRepMaxRecordMaxima(data, 0, &frontier)
	require.Equal(t, []int{0}, frontier.candidateOffsets)
	require.Equal(t, initialHash, frontier.currentHash)
	require.Equal(t, initialHash, frontier.maximumHash)
}
