package cdc

import (
	"encoding/binary"
	"io"
	"math"
)

type wideAsymmetricExtremumContentDefinedChunker struct {
	nonSynchronizableContentDefinedChunker

	regionSizeBytes int
	windowSizeBytes int
	peekSizeBytes   int
}

// NewWideAsymmetricExtremumContentDefinedChunker returns the wide-region
// variant of the Asymmetric Extremum algorithm. It interprets every
// regionSizeBytes-byte region as an unsigned little-endian integer, tracks
// strict record maxima, and retains the first candidate when values are equal.
// Explicit little-endian decoding preserves the WideCDC artifact's x86
// ordering on every architecture.
//
// A chunk ends after the first byte of the terminal region whose start is
// windowSizeBytes positions after the candidate. Bytes after that first byte
// are lookahead and belong to the next chunk. Including the terminal byte
// follows the AE paper's window-size-plus-one minimum and avoids the artifact's
// zero-based length quirk. This portable, paper-consistent implementation also
// examines the final complete region and has no implicit input-buffer cap, so
// it is not boundary-compatible with the evaluation artifact.
//
// regionSizeBytes must be 1, 2, 4, or 8. windowSizeBytes must be positive, and
// their sum must fit in an int. The function panics otherwise.
func NewWideAsymmetricExtremumContentDefinedChunker(
	regionSizeBytes, windowSizeBytes int,
) ContentDefinedChunker {
	switch regionSizeBytes {
	case 1, 2, 4, 8:
	default:
		panic("Wide region size must be 1, 2, 4, or 8 bytes")
	}
	if windowSizeBytes <= 0 {
		panic("Window size must be positive")
	}
	if windowSizeBytes > math.MaxInt-regionSizeBytes {
		panic("Window size is too large")
	}

	// The one-byte variant is exactly AE-Max. Reusing it also retains its
	// optimized search for the maximum byte value.
	if regionSizeBytes == 1 {
		return NewAsymmetricExtremumContentDefinedChunker(windowSizeBytes)
	}

	return &wideAsymmetricExtremumContentDefinedChunker{
		regionSizeBytes: regionSizeBytes,
		windowSizeBytes: windowSizeBytes,
		peekSizeBytes: hashlessChunkerPeekSizeBytes(
			windowSizeBytes + regionSizeBytes - 1,
		),
	}
}

func (c *wideAsymmetricExtremumContentDefinedChunker) NewChunkReader(
	peeker Peeker,
) ChunkReader {
	return &wideAsymmetricExtremumChunkReader{
		contentDefinedChunker: c,
		blocks: finitePeekChunkReader{
			peeker:        peeker,
			peekSizeBytes: c.peekSizeBytes,
		},
	}
}

func (c *wideAsymmetricExtremumContentDefinedChunker) GetMaximumPeekSizeBytes() int {
	return c.peekSizeBytes
}

type wideAsymmetricExtremumChunkReader struct {
	contentDefinedChunker *wideAsymmetricExtremumContentDefinedChunker
	blocks                finitePeekChunkReader
}

func (r *wideAsymmetricExtremumChunkReader) ReadNextChunk() ([]byte, error) {
	if err := r.blocks.beginRead(); err != nil {
		return nil, err
	}

	c := r.contentDefinedChunker
	maximumValue := wideAsymmetricExtremumMaximumValue(c.regionSizeBytes)
	var maximum uint64
	candidateOffset := 0
	blockOffset := 0
	initialized := false

	for {
		data, reachedEOF, err := r.blocks.peekBlock()
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			if len(r.blocks.ownedChunk) == 0 {
				return nil, io.EOF
			}
			return r.blocks.finishChunk(nil, 0)
		}

		// Retain the final regionSizeBytes-1 bytes when refilling. They are
		// lookahead for the first value that starts in the next block.
		valueCount := len(data) - c.regionSizeBytes + 1
		if valueCount <= 0 {
			if reachedEOF {
				return r.blocks.finishChunk(data, len(data))
			}
			return nil, io.ErrNoProgress
		}

		i := 0
		if !initialized {
			maximum = readWideAsymmetricExtremumValue(data, c.regionSizeBytes)
			initialized = true
			i = 1
		}

		for i < valueCount {
			distanceFromCandidate := blockOffset + i - candidateOffset
			valuesThroughDeadline := c.windowSizeBytes - distanceFromCandidate + 1
			end := min(valueCount, i+valuesThroughDeadline)

			if maximum == maximumValue {
				if end-i == valuesThroughDeadline {
					return r.blocks.finishChunk(data, end)
				}
				i = end
				continue
			}

			var cutEnd int
			switch c.regionSizeBytes {
			case 2:
				i, candidateOffset, maximum, cutEnd = scanWideAE16(
					data, i, end, blockOffset, candidateOffset, c.windowSizeBytes, maximum,
				)
			case 4:
				i, candidateOffset, maximum, cutEnd = scanWideAE32(
					data, i, end, blockOffset, candidateOffset, c.windowSizeBytes, maximum,
				)
			case 8:
				i, candidateOffset, maximum, cutEnd = scanWideAE64(
					data, i, end, blockOffset, candidateOffset, c.windowSizeBytes, maximum,
				)
			default:
				panic("Invalid wide region size")
			}
			if cutEnd != 0 {
				return r.blocks.finishChunk(data, cutEnd)
			}
		}

		if reachedEOF {
			return r.blocks.finishChunk(data, len(data))
		}
		blockOffset += valueCount
		if err := r.blocks.consumeBlock(data[:valueCount]); err != nil {
			return nil, err
		}
	}
}

// Width dispatch stays outside the per-region loop. The three explicit loops
// let the compiler specialize each fixed-width little-endian load.
//
// Keep these loops out of ReadNextChunk. Inlining all three creates a large
// combined function and causes unstable frontend code placement on amd64; one
// call per multi-kilobyte scan region is cheaper.
//
//go:noinline
func scanWideAE16(
	data []byte,
	i, end, blockOffset, candidateOffset, windowSizeBytes int,
	maximum uint64,
) (next, updatedCandidate int, updatedMaximum uint64, cutEnd int) {
	_ = data[end]
	for i < end {
		position := blockOffset + i
		value := uint64(binary.LittleEndian.Uint16(data[i : i+2]))
		if value > maximum {
			maximum = value
			candidateOffset = position
		} else if position-candidateOffset == windowSizeBytes {
			return i, candidateOffset, maximum, i + 1
		}
		i++
	}
	return i, candidateOffset, maximum, 0
}

//go:noinline
func scanWideAE32(
	data []byte,
	i, end, blockOffset, candidateOffset, windowSizeBytes int,
	maximum uint64,
) (next, updatedCandidate int, updatedMaximum uint64, cutEnd int) {
	_ = data[end+2]
	for i < end {
		position := blockOffset + i
		value := uint64(binary.LittleEndian.Uint32(data[i : i+4]))
		if value > maximum {
			maximum = value
			candidateOffset = position
		} else if position-candidateOffset == windowSizeBytes {
			return i, candidateOffset, maximum, i + 1
		}
		i++
	}
	return i, candidateOffset, maximum, 0
}

//go:noinline
func scanWideAE64(
	data []byte,
	i, end, blockOffset, candidateOffset, windowSizeBytes int,
	maximum uint64,
) (next, updatedCandidate int, updatedMaximum uint64, cutEnd int) {
	_ = data[end+6]
	for i < end {
		position := blockOffset + i
		value := binary.LittleEndian.Uint64(data[i : i+8])
		if value > maximum {
			maximum = value
			candidateOffset = position
		} else if position-candidateOffset == windowSizeBytes {
			return i, candidateOffset, maximum, i + 1
		}
		i++
	}
	return i, candidateOffset, maximum, 0
}

func wideAsymmetricExtremumMaximumValue(regionSizeBytes int) uint64 {
	switch regionSizeBytes {
	case 1:
		return math.MaxUint8
	case 2:
		return math.MaxUint16
	case 4:
		return math.MaxUint32
	case 8:
		return math.MaxUint64
	default:
		panic("Invalid wide region size")
	}
}

func readWideAsymmetricExtremumValue(data []byte, regionSizeBytes int) uint64 {
	switch regionSizeBytes {
	case 1:
		return uint64(data[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(data))
	case 4:
		return uint64(binary.LittleEndian.Uint32(data))
	case 8:
		return binary.LittleEndian.Uint64(data)
	default:
		panic("Invalid wide region size")
	}
}
