package cdc

import "bytes"

const lexicographicWindowPrefixSizeBytes = 8

// lexicographicWindowPrefix returns the first eight bytes of window as an
// unsigned big-endian integer, or all of window when it is shorter. Numeric
// ordering of the result is therefore the lexicographic ordering of that
// prefix.
func lexicographicWindowPrefix(window []byte) uint64 {
	prefixSize := min(len(window), lexicographicWindowPrefixSizeBytes)
	var prefix uint64
	for _, b := range window[:prefixSize] {
		prefix = prefix<<8 | uint64(b)
	}
	return prefix
}

// scanLexicographicRepMaxRecordMaxima compares the fixed-size windows ending
// at successive cutting points. The first eight bytes are maintained as a
// uint64. Longer windows need a byte-wise comparison only when those prefixes
// are equal.
func scanLexicographicRepMaxRecordMaxima(
	data, maximumWindow []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	if len(maximumWindow) == 0 {
		panic("Missing maximum lexicographic window")
	}
	windowSize := len(maximumWindow)
	incoming := data[windowSize:]

	candidateOffsets := frontier.candidateOffsets
	currentPrefix := frontier.currentHash
	maximumPrefix := frontier.maximumHash

	if windowSize < lexicographicWindowPrefixSizeBytes {
		shift := uint(64 - 8*windowSize)
		mask := ^uint64(0) >> shift
		if maximumPrefix == mask {
			currentPrefix = lexicographicWindowPrefix(data[len(data)-windowSize:])
		} else {
			for i, b := range incoming {
				currentPrefix = currentPrefix<<8 | uint64(b)
				currentPrefix &= mask
				if maximumPrefix >= currentPrefix {
					continue
				}
				maximumPrefix = currentPrefix
				candidateOffsets = append(candidateOffsets, baseOffset+i+1)
				if maximumPrefix == mask {
					currentPrefix = lexicographicWindowPrefix(
						data[len(data)-windowSize:],
					)
					break
				}
			}
		}
	} else {
		for i := range incoming {
			currentPrefix = currentPrefix<<8 | uint64(data[i+lexicographicWindowPrefixSizeBytes])
			if maximumPrefix > currentPrefix {
				continue
			}

			candidateWindow := data[i+1 : i+1+windowSize]
			if maximumPrefix == currentPrefix {
				if windowSize == lexicographicWindowPrefixSizeBytes ||
					bytes.Compare(
						maximumWindow[lexicographicWindowPrefixSizeBytes:],
						candidateWindow[lexicographicWindowPrefixSizeBytes:],
					) >= 0 {
					continue
				}
			}

			maximumPrefix = currentPrefix
			maximumWindow = candidateWindow
			candidateOffsets = append(candidateOffsets, baseOffset+i+1)
		}
	}

	frontier.candidateOffsets = candidateOffsets
	frontier.currentHash = currentPrefix
	frontier.maximumHash = maximumPrefix
}
