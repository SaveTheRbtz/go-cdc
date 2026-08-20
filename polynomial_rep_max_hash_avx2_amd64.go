//go:build amd64 && !purego

package cdc

const (
	polynomialAVX2CandidateBufferSize = 32
	// Limit time spent in the NOSPLIT, non-preemptible assembly scanner.
	polynomialAVX2MaximumScanBytes = 64 << 10
)

func newPolynomialAVX2RepMaxHash() repMaxHash {
	return repMaxHash{
		kind:            repMaxHashPolynomial,
		windowSizeBytes: polynomialHashWindowSizeBytes,
		useAVX2:         true,
	}
}

// scanPolynomialRepMaxRecordMaximaAVX2 has the same state transition as the
// scalar polynomial scanner. The assembly core handles whole four-byte blocks;
// this wrapper grows the candidate slice and handles the final zero to three
// bytes in Go.
func scanPolynomialRepMaxRecordMaximaAVX2(
	data []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	incomingLength := len(data) - polynomialHashWindowSizeBytes
	candidateOffsets := frontier.candidateOffsets
	currentHash := frontier.currentHash
	maximumHash := frontier.maximumHash

	var candidateBuffer [polynomialAVX2CandidateBufferSize]int
	processed := 0
	for vectorBytes := incomingLength &^ 3; processed < vectorBytes; {
		blockLength := vectorBytes - processed
		if blockLength > polynomialAVX2MaximumScanBytes {
			blockLength = polynomialAVX2MaximumScanBytes
		}

		bytesScanned, candidateCount, nextHash, nextMaximum :=
			scanPolynomialRepMaxRecordMaximaAVX2Core(
				data[processed:processed+polynomialHashWindowSizeBytes+blockLength],
				currentHash,
				maximumHash,
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

// scanPolynomialRepMaxRecordMaximaAVX2Core processes a multiple of four
// incoming bytes. Candidate offsets are one-based and relative to data's first
// incoming byte. It may stop early to avoid overflowing candidateOffsets.
//
//go:noescape
func scanPolynomialRepMaxRecordMaximaAVX2Core(
	data []byte,
	currentHash uint64,
	maximumHash uint64,
	candidateOffsets *[polynomialAVX2CandidateBufferSize]int,
) (
	bytesScanned int,
	candidateCount int,
	nextHash uint64,
	nextMaximum uint64,
)
