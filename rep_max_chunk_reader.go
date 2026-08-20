package cdc

import (
	"io"
	"slices"
)

// repMaxFrontier summarizes the hash values already scanned beyond the last
// ready chunk. When initialized, candidate offsets are strict record maxima
// relative to the first eligible cut and candidateOffsets[0] is zero. An empty
// candidate list denotes an uninitialized frontier. currentHash is the hash at
// scannedOffset, while maximumHash is the hash at the final candidate. The
// frontier begins after every queued ready chunk, even before those chunks have
// been returned. scannedOffset is kept separately because it is a cursor, not
// necessarily a candidate.
type repMaxFrontier struct {
	candidateOffsets []int
	scannedOffset    int
	currentHash      uint64
	maximumHash      uint64
}

type repMaxChunkReader struct {
	chunker *repMaxChunker
	peeker  Peeker

	// The previous chunk remains valid until the next call to ReadNextChunk.
	previousChunkSizeBytes int

	// Chunks finalized by the last horizon scan, in reverse return order.
	readyChunkSizes []int

	frontier repMaxFrontier
}

func (r *repMaxChunkReader) ReadNextChunk() ([]byte, error) {
	discardedSizeBytes, err := r.peeker.Discard(r.previousChunkSizeBytes)
	r.previousChunkSizeBytes -= discardedSizeBytes
	if err != nil {
		return nil, err
	}
	if len(r.readyChunkSizes) > 0 {
		last := len(r.readyChunkSizes) - 1
		sizeBytes := r.readyChunkSizes[last]
		data, err := r.peeker.Peek(sizeBytes)
		if err != nil {
			return nil, err
		}
		r.previousChunkSizeBytes = sizeBytes
		r.readyChunkSizes = r.readyChunkSizes[:last]
		return data, nil
	}

	// At EOF, consume everything or leave at least one minimum-sized chunk
	// behind. This keeps all chunks except a short file's only chunk at least
	// minSizeBytes long.
	c := r.chunker
	data, err := r.peeker.Peek(c.peekSizeBytes)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(data) < 2*c.minSizeBytes {
		if len(data) == 0 {
			return nil, io.EOF
		}
		r.previousChunkSizeBytes = len(data)
		return data, nil
	}

	searchData := data[:len(data)-c.minSizeBytes]
	firstChunkSize := r.planChunks(searchData)
	r.previousChunkSizeBytes = firstChunkSize
	return searchData[:firstChunkSize], nil
}

// planChunks scans the available horizon. It may finalize multiple chunks, but
// returns only the first one so the Peeker can release data between calls.
func (r *repMaxChunkReader) planChunks(data []byte) int {
	c := r.chunker
	frontier := r.frontier
	if len(frontier.candidateOffsets) == 0 {
		frontier.candidateOffsets = append(frontier.candidateOffsets[:0], 0)
		frontier.currentHash = c.hash.initialHash(
			data[c.minSizeBytes-c.hash.windowSizeBytes : c.minSizeBytes],
		)
		frontier.maximumHash = frontier.currentHash
	}

	frontier, completedChunkSizes := r.scanHorizon(data, frontier, r.readyChunkSizes)
	var firstChunkSize int
	if len(completedChunkSizes) > 0 {
		// scanHorizon appends ready chunks in return order. Reverse the entire
		// collection once so ReadNextChunk can pop them from the end.
		slices.Reverse(completedChunkSizes)
		last := len(completedChunkSizes) - 1
		firstChunkSize = completedChunkSizes[last]
		completedChunkSizes = completedChunkSizes[:last]
	} else {
		var selectedCandidateIndex int
		firstChunkSize, selectedCandidateIndex = selectForcedChunk(
			frontier.candidateOffsets,
			c.minSizeBytes,
		)
		frontier = r.rebaseFrontier(
			data,
			frontier,
			firstChunkSize,
			selectedCandidateIndex,
		)
	}

	r.frontier = frontier
	r.readyChunkSizes = completedChunkSizes
	return firstChunkSize
}

func (r *repMaxChunkReader) scanHorizon(
	data []byte,
	frontier repMaxFrontier,
	completedChunkSizes []int,
) (repMaxFrontier, []int) {
	c := r.chunker

	// Candidate offsets use an origin that moves when a chunk is finalized.
	// hashEnd remains an absolute data index so the outgoing byte of the
	// rolling window always stays aligned.
	hashEnd := c.minSizeBytes + frontier.scannedOffset
	for hashEnd < len(data) {
		remaining := data[hashEnd:]
		bytesUntilStable := frontier.candidateOffsets[len(frontier.candidateOffsets)-1] +
			c.minSizeBytes - 1 - frontier.scannedOffset
		hashRegion := remaining
		reachesStabilityPoint := len(hashRegion) > bytesUntilStable
		candidateCountBeforeScan := len(frontier.candidateOffsets)
		if reachesStabilityPoint {
			hashRegion = hashRegion[:bytesUntilStable]
		}

		scanData := data[hashEnd-c.hash.windowSizeBytes : hashEnd+len(hashRegion)]
		c.hash.scanRecordMaxima(
			scanData,
			&frontier,
		)
		hashEnd += len(hashRegion)

		if reachesStabilityPoint && len(frontier.candidateOffsets) == candidateCountBeforeScan {
			completedChunkSizes = appendStableChunkSizes(
				completedChunkSizes,
				frontier.candidateOffsets,
				c.minSizeBytes,
			)

			frontier.candidateOffsets = frontier.candidateOffsets[:1]
			frontier.scannedOffset = 0
			frontier.currentHash = c.hash.rollHash(
				frontier.currentHash,
				data[hashEnd-c.hash.windowSizeBytes],
				data[hashEnd],
			)
			hashEnd++
			frontier.maximumHash = frontier.currentHash
		}
	}
	return frontier, completedChunkSizes
}

// appendStableChunkSizes walks record maxima backwards and emits the sequence
// of minimum-sized-or-larger chunks that can no longer change.
func appendStableChunkSizes(completedChunkSizes, candidateOffsets []int, minSizeBytes int) []int {
	firstNewChunk := len(completedChunkSizes)
	nextCut := candidateOffsets[len(candidateOffsets)-1]
	for i := len(candidateOffsets) - 3; nextCut >= minSizeBytes; i-- {
		cut := candidateOffsets[i]
		if sizeBytes := nextCut - cut; sizeBytes >= minSizeBytes {
			completedChunkSizes = append(completedChunkSizes, sizeBytes)
			nextCut = cut
			i--
		}
	}
	completedChunkSizes = append(completedChunkSizes, minSizeBytes+nextCut)
	slices.Reverse(completedChunkSizes[firstNewChunk:])
	return completedChunkSizes
}

// selectForcedChunk walks the record maxima backwards to choose the earliest
// chunk in the recursive RepMax result while respecting the minimum size.
func selectForcedChunk(candidateOffsets []int, minSizeBytes int) (sizeBytes, selectedCandidateIndex int) {
	selectedCandidateIndex = len(candidateOffsets) - 1
	maximumPreviousCut := candidateOffsets[selectedCandidateIndex] - minSizeBytes
	for i := selectedCandidateIndex - 2; maximumPreviousCut >= 0; i-- {
		if cut := candidateOffsets[i]; cut <= maximumPreviousCut {
			selectedCandidateIndex = i
			maximumPreviousCut = cut - minSizeBytes
			i--
		}
	}
	return minSizeBytes + candidateOffsets[selectedCandidateIndex], selectedCandidateIndex
}

// rebaseFrontier drops candidates that are too close to the selected cut and
// expresses the reusable suffix relative to the next chunk. The scan cursor is
// temporarily appended as a sentinel, but never escapes into candidateOffsets.
func (r *repMaxChunkReader) rebaseFrontier(
	data []byte,
	frontier repMaxFrontier,
	firstChunkSize, selectedCandidateIndex int,
) repMaxFrontier {
	points := append(frontier.candidateOffsets, frontier.scannedOffset)
	reusablePointIndex := selectedCandidateIndex + 1
	for {
		offsetInNextChunk := points[reusablePointIndex] - firstChunkSize
		if offsetInNextChunk >= 0 {
			for i := reusablePointIndex; i < len(points); i++ {
				points[i] -= firstChunkSize
			}

			if offsetInNextChunk == 0 {
				points = append(points[:0], points[reusablePointIndex:]...)
			} else {
				points = r.recomputeFrontierPrefix(
					data,
					points,
					firstChunkSize,
					reusablePointIndex,
					offsetInNextChunk,
				)
			}
			break
		}

		reusablePointIndex++
		if reusablePointIndex == len(points) {
			points = points[:1]
			break
		}
	}

	if len(points) < 2 {
		// Nothing beyond the cut was scanned far enough to reuse.
		frontier.candidateOffsets = points[:0]
		frontier.scannedOffset = 0
		frontier.currentHash = 0
		frontier.maximumHash = 0
	} else {
		last := len(points) - 1
		frontier.candidateOffsets = points[:last]
		frontier.scannedOffset = points[last]
	}
	return frontier
}

// recomputeFrontierPrefix restores record maxima between the new origin and
// the first reusable point. This path is rare when the horizon is sufficiently
// large.
func (r *repMaxChunkReader) recomputeFrontierPrefix(
	data []byte,
	points []int,
	firstChunkSize, reusablePointIndex, firstReusableOffset int,
) []int {
	c := r.chunker
	nextChunkData := data[firstChunkSize:][:c.minSizeBytes+firstReusableOffset-1]
	initialHash := c.hash.initialHash(
		nextChunkData[c.minSizeBytes-c.hash.windowSizeBytes : c.minSizeBytes],
	)
	scanData := nextChunkData[c.minSizeBytes-c.hash.windowSizeBytes:]

	// Limit the candidate slice's capacity to the prefix being replaced. If
	// recomputation produces more candidates, append allocates a separate slice
	// and cannot overwrite the reusable suffix in points.
	recomputedFrontier := repMaxFrontier{
		candidateOffsets: points[:1:reusablePointIndex],
		currentHash:      initialHash,
		maximumHash:      initialHash,
	}
	c.hash.scanRecordMaxima(
		scanData,
		&recomputedFrontier,
	)
	recomputed := recomputedFrontier.candidateOffsets
	if len(recomputed) <= reusablePointIndex {
		// recomputed still uses points' backing array. Restore its full capacity
		// before compacting the reusable suffix into place.
		return append(points[:len(recomputed)], points[reusablePointIndex:]...)
	}
	return append(recomputed, points[reusablePointIndex:]...)
}
