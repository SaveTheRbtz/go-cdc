package cdc

import (
	"bytes"
	"io"
	"math"
)

type ramContentDefinedChunker struct {
	nonSynchronizableContentDefinedChunker

	windowSizeBytes int
	peekSizeBytes   int

	// Zero selects canonical, uncapped RAM. RAML sets this to four times the
	// window size.
	maximumChunkSizeBytes int
}

// NewRAMContentDefinedChunker returns a hashless content defined chunker that
// uses the Rapid Asymmetric Maximum algorithm.
//
// RAM finds the largest byte in the first windowSizeBytes bytes of every
// chunk. It then ends the chunk after the first subsequent byte whose value is
// greater than or equal to that maximum. RAM has no artificial maximum chunk
// size, so a chunk may be arbitrarily long. Non-final chunks are at least
// windowSizeBytes+1 bytes long.
//
// windowSizeBytes must be positive and small enough that adding one does not
// overflow an int. The function panics otherwise.
func NewRAMContentDefinedChunker(windowSizeBytes int) ContentDefinedChunker {
	return &ramContentDefinedChunker{
		windowSizeBytes: windowSizeBytes,
		peekSizeBytes:   hashlessChunkerPeekSizeBytes(windowSizeBytes),
	}
}

// NewRAMLContentDefinedChunker returns the length-limited RAM variant. It has
// the same boundary rule as NewRAMContentDefinedChunker, but ends a chunk once
// it reaches four times windowSizeBytes if no content-defined boundary has
// appeared sooner. Non-final chunks remain at least windowSizeBytes+1 bytes
// long.
//
// windowSizeBytes must be positive and small enough that multiplying it by
// four does not overflow an int. The function panics otherwise.
func NewRAMLContentDefinedChunker(windowSizeBytes int) ContentDefinedChunker {
	peekSizeBytes := hashlessChunkerPeekSizeBytes(windowSizeBytes)
	if windowSizeBytes > math.MaxInt/4 {
		panic("Window size is too large")
	}
	return &ramContentDefinedChunker{
		windowSizeBytes:       windowSizeBytes,
		peekSizeBytes:         peekSizeBytes,
		maximumChunkSizeBytes: 4 * windowSizeBytes,
	}
}

func (c *ramContentDefinedChunker) NewChunkReader(peeker Peeker) ChunkReader {
	return &ramChunkReader{
		contentDefinedChunker: c,
		blocks: finitePeekChunkReader{
			peeker:        peeker,
			peekSizeBytes: c.peekSizeBytes,
		},
	}
}

func (c *ramContentDefinedChunker) GetMaximumPeekSizeBytes() int {
	return c.peekSizeBytes
}

type ramChunkReader struct {
	contentDefinedChunker *ramContentDefinedChunker
	blocks                finitePeekChunkReader
}

func (r *ramChunkReader) ReadNextChunk() ([]byte, error) {
	if err := r.blocks.beginRead(); err != nil {
		return nil, err
	}

	windowSizeBytes := r.contentDefinedChunker.windowSizeBytes
	maximumChunkSizeBytes := r.contentDefinedChunker.maximumChunkSizeBytes
	var maximum byte
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

		end := len(data)
		if maximumChunkSizeBytes > 0 {
			end = min(end, maximumChunkSizeBytes-blockOffset)
		}

		i := 0
		if !initialized {
			if end <= windowSizeBytes {
				return r.blocks.finishChunk(data, end)
			}

			window := data[:windowSizeBytes]
			if bytes.IndexByte(window, 0xff) >= 0 {
				maximum = 0xff
			} else {
				for _, b := range window {
					maximum = max(maximum, b)
				}
			}
			initialized = true
			i = windowSizeBytes
		}

		if maximum == 0xff {
			if offset := bytes.IndexByte(data[i:end], 0xff); offset >= 0 {
				return r.blocks.finishChunk(data, i+offset+1)
			}
		} else {
			for offset, b := range data[i:end] {
				if b >= maximum {
					return r.blocks.finishChunk(data, i+offset+1)
				}
			}
		}

		if maximumChunkSizeBytes > 0 && blockOffset+end == maximumChunkSizeBytes {
			return r.blocks.finishChunk(data, end)
		}
		if reachedEOF {
			return r.blocks.finishChunk(data, len(data))
		}
		blockOffset += len(data)
		if err := r.blocks.consumeBlock(data); err != nil {
			return nil, err
		}
	}
}
