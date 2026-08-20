//go:build !goexperiment.simd

package cdc

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

	// Slice the data into eight-byte blocks before indexing it. This lets the
	// compiler eliminate the bounds check for each individual byte.
	i := 0
	for ; i+8 <= len(incoming); i += 8 {
		incomingBlock := incoming[i : i+8]
		outgoingBlock := outgoing[i : i+8]
		hash := currentHash
		hash = rollPolynomialHash(hash, outgoingBlock[0], incomingBlock[0])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+1)
		}
		hash = rollPolynomialHash(hash, outgoingBlock[1], incomingBlock[1])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+2)
		}
		hash = rollPolynomialHash(hash, outgoingBlock[2], incomingBlock[2])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+3)
		}
		hash = rollPolynomialHash(hash, outgoingBlock[3], incomingBlock[3])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+4)
		}
		hash = rollPolynomialHash(hash, outgoingBlock[4], incomingBlock[4])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+5)
		}
		hash = rollPolynomialHash(hash, outgoingBlock[5], incomingBlock[5])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+6)
		}
		hash = rollPolynomialHash(hash, outgoingBlock[6], incomingBlock[6])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+7)
		}
		hash = rollPolynomialHash(hash, outgoingBlock[7], incomingBlock[7])
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+8)
		}
		currentHash = hash
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
