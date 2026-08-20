package cdc

// scanPolynomialRepMaxRecordMaximaNaive is an independent, straightforward
// scanner used to verify the optimized build-selected implementations.
func scanPolynomialRepMaxRecordMaximaNaive(
	data []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	incoming := data[polynomialHashWindowSizeBytes:]
	outgoing := data[:len(incoming)]
	for i, incomingByte := range incoming {
		frontier.currentHash = frontier.currentHash*polynomialHashBase +
			uint64(incomingByte) + polynomialHashByteCoefficientOffset -
			(uint64(outgoing[i])+polynomialHashByteCoefficientOffset)*
				polynomialHashRemovalFactor
		if frontier.maximumHash < frontier.currentHash {
			frontier.maximumHash = frontier.currentHash
			frontier.candidateOffsets = append(
				frontier.candidateOffsets,
				baseOffset+i+1,
			)
		}
	}
}
