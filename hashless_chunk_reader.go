package cdc

import (
	"io"
	"math"
)

const hashlessChunkerMinimumPeekSizeBytes = 64 * 1024

func hashlessChunkerPeekSizeBytes(windowSizeBytes int) int {
	if windowSizeBytes <= 0 {
		panic("Window size must be positive")
	}
	if windowSizeBytes == math.MaxInt {
		panic("Window size is too large")
	}
	return max(hashlessChunkerMinimumPeekSizeBytes, windowSizeBytes+1)
}

// finitePeekChunkReader lets a chunker scan the input in bounded blocks. A
// chunk found in the first block is returned without copying. If scanning has
// to continue, consumed blocks are copied into ownedChunk so that the Peeker
// may discard them before the chunk boundary is known.
type finitePeekChunkReader struct {
	peeker        Peeker
	peekSizeBytes int

	previousChunkSizeBytes int
	ownedChunk             []byte
}

func (r *finitePeekChunkReader) beginRead() error {
	discardedSizeBytes, err := r.peeker.Discard(r.previousChunkSizeBytes)
	r.previousChunkSizeBytes -= discardedSizeBytes
	if err != nil {
		return err
	}
	r.ownedChunk = nil
	return nil
}

func (r *finitePeekChunkReader) peekBlock() (data []byte, reachedEOF bool, err error) {
	data, err = r.peeker.Peek(r.peekSizeBytes)
	if err == nil {
		return data, false, nil
	}
	if err == io.EOF {
		return data, true, nil
	}
	return nil, false, err
}

func (r *finitePeekChunkReader) consumeBlock(data []byte) error {
	r.ownedChunk = append(r.ownedChunk, data...)
	discardedSizeBytes, err := r.peeker.Discard(len(data))
	if err != nil {
		return err
	}
	if discardedSizeBytes != len(data) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func (r *finitePeekChunkReader) finishChunk(data []byte, end int) ([]byte, error) {
	if len(r.ownedChunk) == 0 {
		r.previousChunkSizeBytes = end
		return data[:end], nil
	}

	r.ownedChunk = append(r.ownedChunk, data[:end]...)
	discardedSizeBytes, err := r.peeker.Discard(end)
	if err != nil {
		return nil, err
	}
	if discardedSizeBytes != end {
		return nil, io.ErrUnexpectedEOF
	}
	return r.ownedChunk, nil
}
