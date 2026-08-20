//go:build amd64.v3 && !purego

package cdc

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanPolynomialRepMaxRecordMaximaAVX2(t *testing.T) {
	random := rand.New(rand.NewSource(0x729a4e2b))
	lengths := []int{
		0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17,
		31, 32, 33, 63, 64, 65, 255, 256, 257,
		polynomialAVX2MaximumScanBytes - 1,
		polynomialAVX2MaximumScanBytes,
		polynomialAVX2MaximumScanBytes + 1,
	}
	for range 100 {
		lengths = append(lengths, random.Intn(2*polynomialAVX2MaximumScanBytes))
	}

	for _, length := range lengths {
		data := make([]byte, polynomialHashWindowSizeBytes+length)
		_, err := random.Read(data)
		require.NoError(t, err)

		currentHash := random.Uint64()
		maximumHash := random.Uint64()
		baseOffset := random.Intn(4096)
		scalar := repMaxFrontier{
			candidateOffsets: []int{3, 7},
			currentHash:      currentHash,
			maximumHash:      maximumHash,
		}
		avx2 := repMaxFrontier{
			candidateOffsets: []int{3, 7},
			currentHash:      currentHash,
			maximumHash:      maximumHash,
		}

		scanPolynomialRepMaxRecordMaxima(data, baseOffset, &scalar)
		scanPolynomialRepMaxRecordMaximaAVX2(data, baseOffset, &avx2)
		require.Equal(t, scalar, avx2, "incoming length %d", length)
	}
}

func TestScanPolynomialRepMaxRecordMaximaAVX2CandidateBufferResume(t *testing.T) {
	const baseOffset = 17
	outgoing := [...]byte{
		141, 138, 52, 139, 212, 243, 230, 166,
		206, 55, 158, 255, 158, 133, 142, 163,
		219, 33, 148, 204, 75, 172, 184, 63,
		168, 64, 84, 247, 241, 165, 231, 222,
		202, 118, 44, 187, 13, 125, 125, 71,
	}
	data := make([]byte, polynomialHashWindowSizeBytes+len(outgoing))
	copy(data, outgoing[:])

	scalar := repMaxFrontier{}
	avx2 := repMaxFrontier{}
	scanPolynomialRepMaxRecordMaxima(data, baseOffset, &scalar)
	scanPolynomialRepMaxRecordMaximaAVX2(data, baseOffset, &avx2)
	require.Equal(t, scalar, avx2)

	wantOffsets := make([]int, len(outgoing))
	for i := range wantOffsets {
		wantOffsets[i] = baseOffset + i + 1
	}
	require.Equal(t, wantOffsets, avx2.candidateOffsets)
}

func TestPolyRepMaxContentDefinedChunkerAVX2MatchesScalar(t *testing.T) {
	const (
		minSizeBytes     = 257
		horizonSizeBytes = 8 * minSizeBytes
	)
	data := make([]byte, 1024*1024+31)
	_, err := rand.New(rand.NewSource(5)).Read(data)
	require.NoError(t, err)

	scalar := polynomialRepMaxChunkSizes(
		t,
		data,
		NewPolyRepMaxContentDefinedChunker(minSizeBytes, horizonSizeBytes),
	)
	avx2 := polynomialRepMaxChunkSizes(
		t,
		data,
		NewPolyRepMaxContentDefinedChunkerAVX2(minSizeBytes, horizonSizeBytes),
	)
	require.Equal(t, scalar, avx2)
}
