//go:build !amd64 && goexperiment.simd

package cdc

import "simd"

const (
	// Portable SIMD currently supports vectors up to 512 bits, or eight
	// uint64 lanes.
	polynomialSIMDMaximumUint64Lanes = 8

	// Split the removal factor into pieces that can be multiplied with the
	// portable API's 32-bit lane multiplication.
	polynomialSIMDRemovalLowHalves = (polynomialHashRemovalFactor & 0xffff) |
		((polynomialHashRemovalFactor & 0xffff0000) << 16)
	polynomialSIMDRemovalHigh = polynomialHashRemovalFactor & 0xffffffff00000000
	polynomialSIMDLow32Mask   = uint64(0xffffffff)
)

// scanPolynomialRepMaxRecordMaxima implements the polynomial scanner using
// only the architecture-independent simd package. Hashes and record maxima
// are still consumed in byte order because the hash recurrence is sequential.
func scanPolynomialRepMaxRecordMaxima(
	data []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	incoming := data[polynomialHashWindowSizeBytes:]
	outgoing := data[:len(incoming)]
	candidateOffsets := frontier.candidateOffsets
	currentHash := frontier.currentHash
	maximumHash := frontier.maximumHash

	var outgoingLanes, incomingLanes, adjustments [polynomialSIMDMaximumUint64Lanes]uint64
	var vector simd.Uint64s
	laneCount := vector.Len()
	if laneCount > len(adjustments) {
		panic("unsupported portable SIMD vector width")
	}

	removalLowHalves := simd.BroadcastUint64s(
		polynomialSIMDRemovalLowHalves,
	).ReshapeToUint32s()
	removalHigh := simd.BroadcastUint64s(
		polynomialSIMDRemovalHigh,
	).ReshapeToUint32s()
	low32Mask := simd.BroadcastUint64s(polynomialSIMDLow32Mask)
	rollingAdjustment := simd.BroadcastUint64s(
		polynomialHashRollingAdjustment,
	)

	i := 0
	for ; i+laneCount <= len(incoming); i += laneCount {
		incomingBlock := incoming[i : i+laneCount]
		outgoingBlock := outgoing[i : i+laneCount]
		for lane := range laneCount {
			incomingLanes[lane] = uint64(incomingBlock[lane])
			outgoingLanes[lane] = uint64(outgoingBlock[lane])
		}

		outgoingBytes := simd.LoadUint64s(outgoingLanes[:])
		duplicatedOutgoing := outgoingBytes.Or(
			outgoingBytes.ShiftAllLeft(32),
		).ReshapeToUint32s()

		// For each outgoing byte x, reconstruct x*removalFactor as
		// x*R[0:16] + (x*R[16:32] << 16) + (x*R[32:64] << 32).
		lowParts := duplicatedOutgoing.
			Mul(removalLowHalves).
			ReshapeToUint64s()
		removal := lowParts.And(low32Mask).Add(
			lowParts.ShiftAllRight(32).ShiftAllLeft(16),
		)
		removal = removal.Add(
			duplicatedOutgoing.
				Mul(removalHigh).
				ReshapeToUint64s(),
		)

		simd.LoadUint64s(incomingLanes[:]).
			Add(rollingAdjustment).
			Sub(removal).
			Store(adjustments[:])

		for lane := range laneCount {
			currentHash = currentHash*polynomialHashBase + adjustments[lane]
			if maximumHash < currentHash {
				maximumHash = currentHash
				candidateOffsets = append(
					candidateOffsets,
					baseOffset+i+lane+1,
				)
			}
		}
	}

	for ; i < len(incoming); i++ {
		currentHash = rollPolynomialHash(
			currentHash,
			outgoing[i],
			incoming[i],
		)
		if maximumHash < currentHash {
			maximumHash = currentHash
			candidateOffsets = append(candidateOffsets, baseOffset+i+1)
		}
	}

	frontier.candidateOffsets = candidateOffsets
	frontier.currentHash = currentHash
	frontier.maximumHash = maximumHash
}
