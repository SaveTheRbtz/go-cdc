package cdc

import (
	"bytes"
	"io"
)

type asymmetricExtremumContentDefinedChunker struct {
	nonSynchronizableContentDefinedChunker

	windowSizeBytes int
	peekSizeBytes   int
}

// NewAsymmetricExtremumContentDefinedChunker returns a hashless content
// defined chunker that uses the byte-wise AE-Max algorithm.
//
// Starting at the first byte of every chunk, AE-Max tracks strict record
// maxima. A chunk ends after the first record maximum that is not exceeded by
// any of the next windowSizeBytes bytes. Equal bytes do not replace the
// record maximum. Consequently, chunks are at least windowSizeBytes+1 bytes
// long, except for the final chunk of a short input.
//
// windowSizeBytes must be positive and small enough that adding one does not
// overflow an int. The function panics otherwise.
func NewAsymmetricExtremumContentDefinedChunker(windowSizeBytes int) ContentDefinedChunker {
	return &asymmetricExtremumContentDefinedChunker{
		windowSizeBytes: windowSizeBytes,
		peekSizeBytes:   hashlessChunkerPeekSizeBytes(windowSizeBytes),
	}
}

func (c *asymmetricExtremumContentDefinedChunker) NewChunkReader(peeker Peeker) ChunkReader {
	return &asymmetricExtremumChunkReader{
		contentDefinedChunker: c,
		blocks: finitePeekChunkReader{
			peeker:        peeker,
			peekSizeBytes: c.peekSizeBytes,
		},
	}
}

func (c *asymmetricExtremumContentDefinedChunker) GetMaximumPeekSizeBytes() int {
	return c.peekSizeBytes
}

type asymmetricExtremumChunkReader struct {
	contentDefinedChunker *asymmetricExtremumContentDefinedChunker
	blocks                finitePeekChunkReader
}

func (r *asymmetricExtremumChunkReader) ReadNextChunk() ([]byte, error) {
	if err := r.blocks.beginRead(); err != nil {
		return nil, err
	}

	windowSizeBytes := r.contentDefinedChunker.windowSizeBytes
	var maximum byte
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

		i := 0
		if !initialized {
			maximum = data[0]
			initialized = true
			i = 1
		}

		for i < len(data) {
			// Only scan through the current candidate's deadline. If a
			// later record maximum is found, the next iteration extends it.
			distanceFromCandidate := blockOffset + i - candidateOffset
			bytesThroughDeadline := windowSizeBytes - distanceFromCandidate + 1
			end := min(len(data), i+bytesThroughDeadline)

			if maximum == 0xff {
				if end-i == bytesThroughDeadline {
					return r.blocks.finishChunk(data, end)
				}
				i = end
				continue
			}

			// On typical binary data, the first right window contains
			// 0xff. Finding it in one optimized search lets us skip all
			// intermediate record maxima.
			if offset := bytes.IndexByte(data[i:end], 0xff); offset >= 0 {
				i += offset
				maximum = 0xff
				candidateOffset = blockOffset + i
				i++
				continue
			}

			for i < end {
				position := blockOffset + i
				if b := data[i]; b > maximum {
					maximum = b
					candidateOffset = position
				} else if position-candidateOffset == windowSizeBytes {
					return r.blocks.finishChunk(data, i+1)
				}
				i++
			}
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
