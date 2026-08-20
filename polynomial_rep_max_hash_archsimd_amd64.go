//go:build amd64 && goexperiment.simd

package cdc

import "simd/archsimd"

const polynomialArchSIMDCandidateBufferSize = 32

// scanPolynomialRepMaxRecordMaxima uses archsimd to process four bytes at a
// time. The short suffix is handled here with the scalar rolling hash.
func scanPolynomialRepMaxRecordMaxima(
	data []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	incomingLength := len(data) - polynomialHashWindowSizeBytes
	candidateOffsets := frontier.candidateOffsets
	currentHash := frontier.currentHash
	maximumHash := frontier.maximumHash

	var candidateBuffer [polynomialArchSIMDCandidateBufferSize]int
	processed := 0
	for processed+16 <= incomingLength {
		// Pass the base explicitly so the compiler keeps it in a register
		// across the vector loop.
		bytesScanned, candidateCount, nextHash, nextMaximum :=
			scanPolynomialRepMaxRecordMaximaArchSIMDCore(
				data[processed:],
				currentHash,
				maximumHash,
				polynomialHashBase,
				&candidateBuffer,
			)
		for _, relativeOffset := range candidateBuffer[:candidateCount] {
			candidateOffsets = append(
				candidateOffsets,
				baseOffset+processed+relativeOffset,
			)
		}
		processed += bytesScanned
		currentHash = nextHash
		maximumHash = nextMaximum
	}

	incoming := data[polynomialHashWindowSizeBytes:]
	outgoing := data[:incomingLength]
	for ; processed < incomingLength; processed++ {
		currentHash = rollPolynomialHash(
			currentHash,
			outgoing[processed],
			incoming[processed],
		)
		if maximumHash < currentHash {
			maximumHash = currentHash
			candidateOffsets = append(
				candidateOffsets,
				baseOffset+processed+1,
			)
		}
	}

	frontier.candidateOffsets = candidateOffsets
	frontier.currentHash = currentHash
	frontier.maximumHash = maximumHash
}

// scanPolynomialRepMaxRecordMaximaArchSIMDCore processes four bytes per
// iteration. The 16-byte loads let the compiler fold each load and widening
// conversion into one VPMOVZXBQ instruction. The caller handles the suffix for
// which a full load would cross the end of the slice.
func scanPolynomialRepMaxRecordMaximaArchSIMDCore(
	data []byte,
	currentHash uint64,
	maximumHash uint64,
	hashBase uint64,
	candidateOffsets *[polynomialArchSIMDCandidateBufferSize]int,
) (
	bytesScanned int,
	candidateCount int,
	nextHash uint64,
	nextMaximum uint64,
) {
	removalLow := archsimd.BroadcastUint32x8(
		uint32(polynomialHashRemovalFactor & 0xffffffff),
	)
	removalHigh := archsimd.BroadcastUint32x8(
		uint32(polynomialHashRemovalFactor >> 32),
	)
	rollingAdjustment := archsimd.BroadcastUint64x4(
		polynomialHashRollingAdjustment,
	)

	var adjustments [4]uint64
	i := 0
	for len(data) >= polynomialHashWindowSizeBytes+16 {
		// A block can produce at most four record maxima. Stop before there
		// is any chance of overflowing the fixed candidate buffer.
		if candidateCount > len(candidateOffsets)-4 {
			break
		}

		incomingBytes := archsimd.LoadUint8x16(
			data[polynomialHashWindowSizeBytes : polynomialHashWindowSizeBytes+16],
		)
		outgoingBytes := archsimd.LoadUint8x16(data[:16])
		incomingValues := incomingBytes.ExtendLo4ToUint64()
		outgoingValues := outgoingBytes.
			ExtendLo4ToUint64().
			ReshapeToUint32s()

		// An outgoing byte has no high 32-bit half, so its product modulo
		// 2^64 needs only the low-low and low-high 32-bit products.
		outgoingProduct := outgoingValues.MulWidenEven(removalLow).Add(
			outgoingValues.
				MulWidenEven(removalHigh).
				ShiftAllLeft(32),
		)
		incomingValues.
			Add(rollingAdjustment).
			Sub(outgoingProduct).
			StoreArray(&adjustments)

		// Keeping the record branch separate from the maximum update makes
		// the latter a conditional move and keeps both hashes in fixed
		// registers across all four steps.
		currentHash = currentHash*hashBase + adjustments[0]
		if maximumHash < currentHash {
			candidateOffsets[candidateCount] = i + 1
			candidateCount++
		}
		maximumHash = max(maximumHash, currentHash)
		currentHash = currentHash*hashBase + adjustments[1]
		if maximumHash < currentHash {
			candidateOffsets[candidateCount] = i + 2
			candidateCount++
		}
		maximumHash = max(maximumHash, currentHash)
		currentHash = currentHash*hashBase + adjustments[2]
		if maximumHash < currentHash {
			candidateOffsets[candidateCount] = i + 3
			candidateCount++
		}
		maximumHash = max(maximumHash, currentHash)
		currentHash = currentHash*hashBase + adjustments[3]
		if maximumHash < currentHash {
			candidateOffsets[candidateCount] = i + 4
			candidateCount++
		}
		maximumHash = max(maximumHash, currentHash)
		i += 4
		// Advance the outgoing and incoming views together.
		data = data[4:]
	}

	// Go does not yet insert VZEROUPPER automatically. Clear the upper lanes
	// before the caller resumes ordinary Go code and may grow a slice.
	archsimd.ClearAVXUpperBits()
	return i, candidateCount, currentHash, maximumHash
}
