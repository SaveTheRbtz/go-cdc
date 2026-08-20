package cdc

import "io"

type seqContentDefinedChunker struct {
	nonSynchronizableContentDefinedChunker

	sequenceLength        int
	skipTrigger           int
	skipSizeBytes         int
	minimumChunkSizeBytes int
	maximumChunkSizeBytes int
}

// NewSeqContentDefinedChunker returns the increasing-mode SeqCDC parameter
// set targeting the requested average chunk size. The supported average sizes
// are 4 KiB, 8 KiB, and 16 KiB.
//
// SeqCDC ends a chunk after five strictly increasing bytes. After enough
// decreasing comparisons, SeqCDC skips a short region of input. Every
// non-final chunk is limited to the minimum and maximum sizes selected by the
// paper; a short final chunk is returned as-is.
//
// This implementation interprets increasing sequences strictly, so equality
// starts a new sequence. Sequence length counts bytes, the earliest boundary
// is exactly the minimum chunk size, and the terminal byte belongs to the
// ending chunk. The DedupBench C++ implementation instead starts scanning at
// the minimum, counts comparisons, absorbs equality, and returns the terminal
// byte's index as a length. Those implementation choices are intentionally not
// reproduced.
//
// The function panics for unsupported normal chunk sizes.
func NewSeqContentDefinedChunker(averageSizeBytes int) ContentDefinedChunker {
	c := &seqContentDefinedChunker{
		sequenceLength: 5,
	}
	switch averageSizeBytes {
	case 4 * 1024:
		c.skipTrigger = 55
		c.skipSizeBytes = 256
		c.minimumChunkSizeBytes = 1024
		c.maximumChunkSizeBytes = 8192
	case 8 * 1024:
		c.skipTrigger = 50
		c.skipSizeBytes = 256
		c.minimumChunkSizeBytes = 4096
		c.maximumChunkSizeBytes = 16384
	case 16 * 1024:
		c.skipTrigger = 50
		c.skipSizeBytes = 512
		c.minimumChunkSizeBytes = 8192
		c.maximumChunkSizeBytes = 32768
	default:
		panic("SeqCDC only supports average chunk sizes of 4 KiB, 8 KiB, and 16 KiB")
	}
	return c
}

func (c *seqContentDefinedChunker) NewChunkReader(peeker Peeker) ChunkReader {
	return &seqChunkReader{
		contentDefinedChunker: c,
		peeker:                peeker,
	}
}

func (c *seqContentDefinedChunker) GetMaximumPeekSizeBytes() int {
	return c.maximumChunkSizeBytes
}

type seqChunkReader struct {
	contentDefinedChunker *seqContentDefinedChunker
	peeker                Peeker

	previousChunkSizeBytes int
}

func (r *seqChunkReader) ReadNextChunk() ([]byte, error) {
	// Discard data that was handed out by the previous call.
	discardedSizeBytes, err := r.peeker.Discard(r.previousChunkSizeBytes)
	r.previousChunkSizeBytes -= discardedSizeBytes
	if err != nil {
		return nil, err
	}

	c := r.contentDefinedChunker
	data, err := r.peeker.Peek(c.maximumChunkSizeBytes)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(data) == 0 {
		return nil, io.EOF
	}

	if len(data) <= c.minimumChunkSizeBytes {
		r.previousChunkSizeBytes = len(data)
		return data, nil
	}

	// The paper ignores minimumChunkSizeBytes-sequenceLength bytes. The byte
	// immediately after that region seeds the first possible sequence, so a
	// qualifying sequence may end exactly at the minimum chunk size.
	position := c.minimumChunkSizeBytes - c.sequenceLength + 1
	increasingLength := 1
	opposingSlopeCount := 0
	for position < len(data) {
		previous := data[position-1]
		current := data[position]
		position++

		if current > previous {
			increasingLength++
			if increasingLength == c.sequenceLength {
				r.previousChunkSizeBytes = position
				return data[:position], nil
			}
			continue
		}

		increasingLength = 1
		if current < previous {
			opposingSlopeCount++
			if opposingSlopeCount == c.skipTrigger {
				// Ignore the next skipSizeBytes bytes. The first byte after
				// them starts a fresh sequence, so the following comparison
				// begins one byte later.
				position += c.skipSizeBytes + 1
				opposingSlopeCount = 0
			}
		}
	}

	r.previousChunkSizeBytes = len(data)
	return data, nil
}
