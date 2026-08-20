package cdc

// For a window b[0] through b[63], the polynomial hash is
//
//	(b[0]+1)*base^63 + ... + (b[63]+1) mod 2^64.
//
// Go's uint64 arithmetic performs the required reduction modulo 2^64.
const (
	// polynomialHashWindowSizeBytes is the number of input bytes covered by
	// the polynomial rolling hash used by PolynomialRepMaxCDC.
	polynomialHashWindowSizeBytes = 64

	// polynomialHashBase is the multiplier used by the polynomial rolling
	// hash. Keeping this value fixed makes cutting points interoperable across
	// implementations and processes.
	polynomialHashBase uint64 = 0x9e3779b97f4a7c15

	// polynomialHashByteCoefficientOffset is added to every input byte before
	// it is incorporated into the polynomial hash. In other words, byte b has
	// coefficient uint64(b)+polynomialHashByteCoefficientOffset.
	polynomialHashByteCoefficientOffset uint64 = 1

	// polynomialHashRemovalFactor is polynomialHashBase raised to
	// polynomialHashWindowSizeBytes modulo 2^64. It is multiplied by the
	// outgoing coefficient when advancing the rolling hash by one byte.
	polynomialHashRemovalFactor uint64 = 0x66c0333b9c3b3301

	// polynomialHashRollingAdjustment combines the coefficient offsets of the
	// incoming and outgoing bytes: 1-polynomialHashRemovalFactor modulo 2^64.
	polynomialHashRollingAdjustment uint64 = 0x993fccc463c4cd00
)

// computePolynomialHash computes the polynomial hash of data using Horner's
// method. Arithmetic on uint64 values intentionally wraps modulo 2^64.
func computePolynomialHash(data []byte) uint64 {
	var hash uint64
	for _, b := range data {
		hash = hash*polynomialHashBase + uint64(b) + polynomialHashByteCoefficientOffset
	}
	return hash
}

// rollPolynomialHash advances a hash covering one complete window by one
// byte. Arithmetic on uint64 values intentionally wraps modulo 2^64.
func rollPolynomialHash(hash uint64, outgoing, incoming byte) uint64 {
	return hash*polynomialHashBase + uint64(incoming) + polynomialHashByteCoefficientOffset -
		(uint64(outgoing)+polynomialHashByteCoefficientOffset)*polynomialHashRemovalFactor
}

// polynomialRollingHash computes the polynomial hash when the preceding
// window is not available as a contiguous slice. This is only needed while
// searching for a guaranteed chunk boundary, where input is discarded as the
// search progresses.
type polynomialRollingHash struct {
	hash       uint64
	window     [polynomialHashWindowSizeBytes]byte
	next       int
	windowSize int
}

func (h *polynomialRollingHash) addByte(b byte) uint64 {
	if h.windowSize < polynomialHashWindowSizeBytes {
		h.hash = h.hash*polynomialHashBase + uint64(b) + polynomialHashByteCoefficientOffset
		h.windowSize++
	} else {
		h.hash = rollPolynomialHash(h.hash, h.window[h.next], b)
	}
	h.window[h.next] = b
	h.next = (h.next + 1) & (polynomialHashWindowSizeBytes - 1)
	return h.hash
}
