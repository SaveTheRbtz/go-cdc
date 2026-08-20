package cdc

import (
	"io"
	"math"
)

type repMaxChunker struct {
	hash                               repMaxHash
	minSizeBytes                       int
	peekSizeBytes                      int
	supportsDiscardUpToGuaranteedChunk bool
}

// NewRepMaxContentDefinedChunker returns a content defined chunker that
// expands upon MaxCDC, in that it repeatedly applies the chunking process
// until chunks are [minSizeBytes, 2*minSizeBytes) in size.
//
// Like MaxCDC, this algorithm takes a parameter that controls the amount of
// data that is read ahead. While MaxCDC uses it to control the maximum chunk
// size, in this algorithm it only denotes the quality of the chunking that is
// performed (i.e., the horizon size). Setting it to zero leads to uniform
// chunking of minSizeBytes, while setting it to a positive value n means that
// an optimal point within offsets [minSizeBytes, minSizeBytes+n] will always be
// respected.
//
// While MaxCDC performs poorly if the ratio between the maximum and minimum
// chunk size becomes too large, the horizon size can be increased freely
// without reducing quality. However, there will be diminishing returns.
//
// It has been observed that this algorithm provides an almost identical rate
// of deduplication as MaxCDC. The advantage of this algorithm over MaxCDC is
// that for a given input it is trivial to check whether it is already chunked,
// purely looking at its size.
func NewRepMaxContentDefinedChunker(
	gearTable *GearTable,
	minSizeBytes, horizonSizeBytes int,
) ContentDefinedChunker {
	return newRepMaxChunker(
		newGearRepMaxHash(gearTable),
		minSizeBytes,
		horizonSizeBytes,
	)
}

// NewPolyRepMaxContentDefinedChunker returns a content defined chunker that
// behaves like NewRepMaxContentDefinedChunker, but uses a polynomial rolling
// hash modulo 2^64 instead of a Gear hash.
//
// The hash covers 64 bytes, uses 0x9e3779b97f4a7c15 as its base, and maps
// every byte b to the coefficient uint64(b)+1. At cutting point i, the oldest
// byte in data[i-64:i] has exponent 63 and data[i] is not included. Hashes are
// compared as unsigned 64-bit integers.
//
// Builds made with GOEXPERIMENT=simd use the experimental simd/archsimd
// package on amd64 and the portable simd package on other architectures.
// Builds without the experiment use the scalar scanner. All implementations
// produce identical cutting points. The amd64 implementation requires an
// AVX2-capable processor and performs no runtime CPU detection.
//
// minSizeBytes must be at least 64, horizonSizeBytes must be non-negative, and
// 2*minSizeBytes+horizonSizeBytes must fit in an int. The function panics if
// these requirements are not met.
func NewPolyRepMaxContentDefinedChunker(minSizeBytes, horizonSizeBytes int) ContentDefinedChunker {
	return newPolyRepMaxContentDefinedChunker(
		newPolynomialRepMaxHash(),
		minSizeBytes,
		horizonSizeBytes,
	)
}

func newPolyRepMaxContentDefinedChunker(
	hash repMaxHash,
	minSizeBytes, horizonSizeBytes int,
) ContentDefinedChunker {
	if minSizeBytes < polynomialHashWindowSizeBytes {
		panic("Minimum chunk size is smaller than the polynomial hash window")
	}
	if horizonSizeBytes < 0 {
		panic("Horizon size is negative")
	}
	if minSizeBytes > (math.MaxInt-horizonSizeBytes)/2 {
		panic("Minimum chunk size and horizon size are too large")
	}
	return newRepMaxChunker(
		hash,
		minSizeBytes,
		horizonSizeBytes,
	)
}

func newRepMaxChunker(
	hash repMaxHash,
	minSizeBytes, horizonSizeBytes int,
) *repMaxChunker {
	return &repMaxChunker{
		hash:                               hash,
		minSizeBytes:                       minSizeBytes,
		peekSizeBytes:                      2*minSizeBytes + horizonSizeBytes,
		supportsDiscardUpToGuaranteedChunk: horizonSizeBytes >= 2*(minSizeBytes-1),
	}
}

func (c *repMaxChunker) NewChunkReader(peeker Peeker) ChunkReader {
	return &repMaxChunkReader{
		chunker:         c,
		peeker:          peeker,
		readyChunkSizes: make([]int, 0, c.peekSizeBytes/c.minSizeBytes),
		// Even though this list can grow to become proportional to the size of
		// the horizon, finding each additional record maximum becomes less
		// likely. A capacity of 32 covers virtually all inputs.
		frontier: repMaxFrontier{
			candidateOffsets: make([]int, 0, 32),
		},
	}
}

func (c *repMaxChunker) SupportsDiscardUpToGuaranteedChunk() bool {
	return c.supportsDiscardUpToGuaranteedChunk
}

// repMaxStreamingHash retains the additional state needed when earlier input
// may already have been discarded. It is used only while locating guaranteed
// chunk boundaries.
type repMaxStreamingHash struct {
	hash       repMaxHash
	gearValue  uint64
	polynomial polynomialRollingHash
}

func (h *repMaxStreamingHash) addByte(b byte) uint64 {
	switch h.hash.kind {
	case repMaxHashGear:
		h.gearValue = (h.gearValue << 1) + h.hash.gearValues[b]
		return h.gearValue
	case repMaxHashPolynomial:
		return h.polynomial.addByte(b)
	default:
		panic("Unknown RepMax hash")
	}
}

// scanUntilHashExceeds updates the rolling hash over data. It stops after the
// first new record maximum that exceeds limit. Hash-kind dispatch happens once
// per region, not once per byte.
func (h *repMaxStreamingHash) scanUntilHashExceeds(
	data []byte,
	maximumHash, limit uint64,
) (bytesScanned int, updatedMaximum uint64, exceeded bool) {
	switch h.hash.kind {
	case repMaxHashGear:
		value := h.gearValue
		gearValues := h.hash.gearValues
		// Hoist the pointer check out of the byte loop.
		_ = gearValues[0]
		for i, b := range data {
			value = (value << 1) + gearValues[b]
			if maximumHash < value {
				maximumHash = value
				if limit < value {
					h.gearValue = value
					return i + 1, maximumHash, true
				}
			}
		}
		h.gearValue = value
	case repMaxHashPolynomial:
		// Passing the constants into the non-inlined helper keeps them in
		// registers throughout its steady-state loop.
		return scanPolynomialHashUntilExceeds(
			&h.polynomial,
			data,
			maximumHash,
			limit,
			polynomialHashBase,
			polynomialHashRemovalFactor,
			polynomialHashRollingAdjustment,
		)
	default:
		panic("Unknown RepMax hash")
	}
	return len(data), maximumHash, false
}

func scanPolynomialHashUntilExceeds(
	hash *polynomialRollingHash,
	data []byte,
	maximumHash, limit,
	base, removalFactor, rollingAdjustment uint64,
) (bytesScanned int, updatedMaximum uint64, exceeded bool) {
	value := hash.hash
	next := hash.next
	windowSize := hash.windowSize

	// Fill the initial window before entering the steady-state rolling loop.
	// Keeping these phases separate removes a window-size branch from every
	// subsequent byte.
	i := 0
	for i < len(data) && windowSize < polynomialHashWindowSizeBytes {
		b := data[i]
		value = value*base + uint64(b) + polynomialHashByteCoefficientOffset
		windowSize++
		hash.window[next] = b
		next = (next + 1) & (polynomialHashWindowSizeBytes - 1)
		if maximumHash < value {
			maximumHash = value
			if limit < value {
				hash.hash = value
				hash.next = next
				hash.windowSize = windowSize
				return i + 1, maximumHash, true
			}
		}
		i++
	}
	if i == len(data) {
		hash.hash = value
		hash.next = next
		hash.windowSize = windowSize
		return len(data), maximumHash, false
	}

	for j, b := range data[i:] {
		value = value*base + uint64(b) - uint64(hash.window[next])*removalFactor +
			rollingAdjustment
		hash.window[next] = b
		next = (next + 1) & (polynomialHashWindowSizeBytes - 1)
		if maximumHash < value {
			maximumHash = value
			if limit < value {
				hash.hash = value
				hash.next = next
				hash.windowSize = polynomialHashWindowSizeBytes
				return i + j + 1, maximumHash, true
			}
		}
	}
	hash.hash = value
	hash.next = next
	hash.windowSize = polynomialHashWindowSizeBytes
	return len(data), maximumHash, false
}

func (c *repMaxChunker) DiscardUpToGuaranteedChunk(peeker Peeker) error {
	if !c.supportsDiscardUpToGuaranteedChunk {
		panic("Horizon size is too small to permit discarding up to a guaranteed chunk")
	}

	// Divide the input into regions of minSizeBytes-1 points and retain only
	// the largest hash in each region. A candidate is guaranteed once its hash
	// exceeds the maxima in the relevant neighboring regions.
	hash := repMaxStreamingHash{hash: c.hash}
	maximumCurrent := ^uint64(0)
	maximumNext := ^uint64(0)
	bytesUntilNextRegion := c.hash.windowSizeBytes - 1

	for {
		// A cutting point is valid only when at least minSizeBytes remain after
		// it. Peek minSizeBytes, but process only minSizeBytes-1 points.
		data, err := peeker.Peek(c.minSizeBytes)
		if err != nil && err != io.EOF {
			return err
		}
		if len(data) < c.minSizeBytes {
			return io.EOF
		}

		// Finish the trailing portion of the current region.
		bytesScanned, updatedMaximumNext, exceeded := hash.scanUntilHashExceeds(
			data[:bytesUntilNextRegion],
			maximumNext,
			maximumCurrent,
		)
		maximumNext = updatedMaximumNext
		if exceeded {
			if _, err := peeker.Discard(bytesScanned); err != nil {
				return err
			}
			bytesUntilNextRegion -= bytesScanned
			continue
		}

		// Enter the next region and process its first byte.
		value := hash.addByte(data[bytesUntilNextRegion])
		maximumPrevious := maximumCurrent
		maximumCurrent, maximumNext = maximumNext, value
		if maximumPrevious >= maximumCurrent || maximumCurrent < maximumNext {
			if _, err := peeker.Discard(bytesUntilNextRegion + 1); err != nil {
				return err
			}
			bytesUntilNextRegion = c.minSizeBytes - 2
			continue
		}

		// Inspect the leading portion of the next region.
		bytesScanned, maximumNext, exceeded = hash.scanUntilHashExceeds(
			data[bytesUntilNextRegion+1:c.minSizeBytes-1],
			maximumNext,
			maximumCurrent,
		)
		if !exceeded {
			return nil
		}
		if _, err := peeker.Discard(bytesUntilNextRegion + 1 + bytesScanned); err != nil {
			return err
		}
		bytesUntilNextRegion = c.minSizeBytes - bytesScanned - 2
	}
}

func (c *repMaxChunker) GetMaximumPeekSizeBytes() int {
	return c.peekSizeBytes
}
