//go:build amd64 && goexperiment.simd

package cdc

import (
	"math/rand"
	"simd/archsimd"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScanPolynomialRepMaxRecordMaximaArchSIMD(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 is unavailable")
	}
	random := rand.New(rand.NewSource(0x39c42f7a))
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

		currentHash := random.Uint64()
		maximumHash := random.Uint64()
		baseOffset := random.Intn(4096)
		scalar := repMaxFrontier{
			candidateOffsets: []int{3, 7},
			currentHash:      currentHash,
			maximumHash:      maximumHash,
		}
		archSIMD := repMaxFrontier{
			candidateOffsets: []int{3, 7},
			currentHash:      currentHash,
			maximumHash:      maximumHash,
		}

		scanPolynomialRepMaxRecordMaximaScalar(data, baseOffset, &scalar)
		scanPolynomialRepMaxRecordMaxima(data, baseOffset, &archSIMD)
		require.Equal(t, scalar, archSIMD, "incoming length %d", length)
	}
}

func TestScanPolynomialRepMaxRecordMaximaArchSIMDCandidateBufferResume(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 is unavailable")
	}
	const baseOffset = 17
	outgoing := [...]byte{
		141, 138, 52, 139, 212, 243, 230, 166,
		206, 55, 158, 255, 158, 133, 142, 163,
		219, 33, 148, 204, 75, 172, 184, 63,
		168, 64, 84, 247, 241, 165, 231, 222,
		202, 118, 44, 187, 13, 125, 125, 71,
	}
	data := make([]byte, polynomialHashWindowSizeBytes+len(outgoing)+16)
	copy(data, outgoing[:])

	scalar := repMaxFrontier{}
	archSIMD := repMaxFrontier{}
	scanPolynomialRepMaxRecordMaximaScalar(data, baseOffset, &scalar)
	scanPolynomialRepMaxRecordMaxima(data, baseOffset, &archSIMD)
	require.Equal(t, scalar, archSIMD)

	wantOffsets := make([]int, len(outgoing))
	for i := range wantOffsets {
		wantOffsets[i] = baseOffset + i + 1
	}
	require.Equal(t, wantOffsets, archSIMD.candidateOffsets[:len(wantOffsets)])
}

func TestScanPolynomialRepMaxRecordMaximaArchSIMDEqualHashes(t *testing.T) {
	if !archsimd.X86.AVX2() {
		t.Skip("AVX2 is unavailable")
	}
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
