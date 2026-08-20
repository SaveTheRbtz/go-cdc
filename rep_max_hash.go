package cdc

// repMaxHash is the immutable hash configuration used by the shared RepMaxCDC
// engine. It is deliberately concrete: Gear and polynomial hashing are tightly
// coupled to the scanner, and the package does not expose custom hash variants.
type repMaxHash struct {
	kind            repMaxHashKind
	windowSizeBytes int
	gearValues      *[256]uint64
}

type repMaxHashKind uint8

const (
	repMaxHashInvalid repMaxHashKind = iota
	repMaxHashGear
	repMaxHashPolynomial
)

func newGearRepMaxHash(gearTable *GearTable) repMaxHash {
	return repMaxHash{
		kind:            repMaxHashGear,
		windowSizeBytes: gearHashWindowSizeBytes,
		gearValues:      &gearTable.values,
	}
}

func newPolynomialRepMaxHash() repMaxHash {
	return repMaxHash{
		kind:            repMaxHashPolynomial,
		windowSizeBytes: polynomialHashWindowSizeBytes,
	}
}

// initialHash computes the hash of exactly one complete hash window.
func (h repMaxHash) initialHash(window []byte) uint64 {
	switch h.kind {
	case repMaxHashGear:
		var hash uint64
		for _, b := range window {
			hash = (hash << 1) + h.gearValues[b]
		}
		return hash
	case repMaxHashPolynomial:
		return computePolynomialHash(window)
	default:
		panic("Unknown RepMax hash")
	}
}

func (h repMaxHash) rollHash(hash uint64, outgoing, incoming byte) uint64 {
	switch h.kind {
	case repMaxHashGear:
		return (hash << 1) + h.gearValues[incoming]
	case repMaxHashPolynomial:
		return rollPolynomialHash(hash, outgoing, incoming)
	default:
		panic("Unknown RepMax hash")
	}
}

// scanRecordMaxima advances frontier over data. The prefix of data is the
// current hash window; the remaining bytes are the incoming bytes to scan.
// A record at incoming byte i is stored at scannedOffset+i+1. Comparisons are
// strict so equal hashes retain the leftmost cut.
func (h repMaxHash) scanRecordMaxima(
	data []byte,
	frontier *repMaxFrontier,
) {
	incoming := data[h.windowSizeBytes:]
	switch h.kind {
	case repMaxHashGear:
		scanGearRepMaxRecordMaxima(
			h.gearValues,
			incoming,
			frontier.scannedOffset,
			frontier,
		)
	case repMaxHashPolynomial:
		scanPolynomialRepMaxRecordMaxima(
			data,
			frontier.scannedOffset,
			frontier,
		)
	default:
		panic("Unknown RepMax hash")
	}
	frontier.scannedOffset += len(incoming)
}

// scanGearRepMaxRecordMaxima is kept separate from the shared scanner so its
// manually unrolled hot loop remains direct and easy to profile.
func scanGearRepMaxRecordMaxima(
	values *[256]uint64,
	incoming []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	candidateOffsets := frontier.candidateOffsets
	currentHash := frontier.currentHash
	maximumHash := frontier.maximumHash

	// The loop is unrolled manually, as the Go compiler does not do it.
	// Eight was empirically determined to give good performance.
	i := 0
	for ; i+8 <= len(incoming); i += 8 {
		b := [8]byte(incoming[i : i+8])
		s := values[b[0]]
		hash := (currentHash << 1) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+1)
		}
		s = (s << 1) + values[b[1]]
		hash = (currentHash << 2) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+2)
		}
		s = (s << 1) + values[b[2]]
		hash = (currentHash << 3) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+3)
		}
		s = (s << 1) + values[b[3]]
		hash = (currentHash << 4) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+4)
		}
		s = (s << 1) + values[b[4]]
		hash = (currentHash << 5) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+5)
		}
		s = (s << 1) + values[b[5]]
		hash = (currentHash << 6) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+6)
		}
		s = (s << 1) + values[b[6]]
		hash = (currentHash << 7) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+7)
		}
		s = (s << 1) + values[b[7]]
		hash = (currentHash << 8) + s
		if maximumHash < hash {
			maximumHash = hash
			candidateOffsets = append(candidateOffsets, baseOffset+i+8)
		}
		currentHash = hash
	}
	for ; i < len(incoming); i++ {
		currentHash = (currentHash << 1) + values[incoming[i]]
		if maximumHash < currentHash {
			maximumHash = currentHash
			candidateOffsets = append(candidateOffsets, baseOffset+i+1)
		}
	}

	frontier.candidateOffsets = candidateOffsets
	frontier.currentHash = currentHash
	frontier.maximumHash = maximumHash
}

// scanPolynomialRepMaxRecordMaxima contains the only deliberately unrolled
// polynomial code in the package.
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
