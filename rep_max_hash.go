package cdc

// repMaxHash is the immutable ordering configuration used by the shared
// RepMaxCDC engine. It is deliberately concrete: every supported ordering has
// a scanner tailored to its representation.
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
	repMaxHashLexicographic
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

func newLexicographicRepMaxHash(windowSizeBytes int) repMaxHash {
	return repMaxHash{
		kind:            repMaxHashLexicographic,
		windowSizeBytes: windowSizeBytes,
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
	case repMaxHashLexicographic:
		return lexicographicWindowPrefix(window)
	default:
		panic("Unknown RepMax hash")
	}
}

// rollHash slides the stored comparison key by one byte. For lexicographic
// windows longer than eight bytes, entering is the byte entering the stored
// eight-byte prefix, rather than the byte entering the end of the window.
func (h repMaxHash) rollHash(hash uint64, outgoing, entering byte) uint64 {
	switch h.kind {
	case repMaxHashGear:
		return (hash << 1) + h.gearValues[entering]
	case repMaxHashPolynomial:
		return rollPolynomialHash(hash, outgoing, entering)
	case repMaxHashLexicographic:
		hash = hash<<8 | uint64(entering)
		if h.windowSizeBytes < lexicographicWindowPrefixSizeBytes {
			return hash & (^uint64(0) >> uint(64-8*h.windowSizeBytes))
		}
		return hash
	default:
		panic("Unknown RepMax hash")
	}
}

// scanRecordMaxima advances frontier over data. The prefix of data is the
// current score window; the remaining bytes are the incoming bytes to scan.
// A record at incoming byte i is stored at scannedOffset+i+1. Comparisons are
// strict so equal scores retain the leftmost cut. maximumWindow is only used
// by the lexicographic scanner when two windows have equal 64-bit prefixes.
func (h repMaxHash) scanRecordMaxima(
	data, maximumWindow []byte,
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
	case repMaxHashLexicographic:
		scanLexicographicRepMaxRecordMaxima(
			data,
			maximumWindow,
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
