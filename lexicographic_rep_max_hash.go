package cdc

import (
	"bytes"
	"encoding/binary"
)

const lexicographicWindowPrefixSizeBytes = 8

// lexicographicWindowPrefix returns the first eight bytes of window as an
// unsigned big-endian integer, or all of window when it is shorter. Numeric
// ordering of the result is therefore the lexicographic ordering of that
// prefix.
func lexicographicWindowPrefix(window []byte) uint64 {
	prefixSize := min(len(window), lexicographicWindowPrefixSizeBytes)
	if prefixSize == lexicographicWindowPrefixSizeBytes {
		return binary.BigEndian.Uint64(window)
	}
	var prefix uint64
	for _, b := range window[:prefixSize] {
		prefix = prefix<<8 | uint64(b)
	}
	return prefix
}

// lexicographicRepMaxScan holds the state needed only when a window's first
// byte could reach the current maximum. Keeping that path out of the scanner's
// main loop lets the common first-byte comparison stay small.
type lexicographicRepMaxScan struct {
	candidateOffsets []int
	maximumWindow    []byte
	maximumPrefix    uint64
	baseOffset       int
}

//go:noinline
func (s *lexicographicRepMaxScan) considerWindow(data []byte, start int) bool {
	windowSize := len(s.maximumWindow)
	candidateWindow := data[start : start+windowSize]
	candidatePrefix := lexicographicWindowPrefix(candidateWindow)
	if s.maximumPrefix > candidatePrefix {
		return false
	}
	if s.maximumPrefix == candidatePrefix &&
		(windowSize <= lexicographicWindowPrefixSizeBytes ||
			bytes.Compare(
				s.maximumWindow[lexicographicWindowPrefixSizeBytes:],
				candidateWindow[lexicographicWindowPrefixSizeBytes:],
			) >= 0) {
		return false
	}

	s.maximumPrefix = candidatePrefix
	s.maximumWindow = candidateWindow
	s.candidateOffsets = append(s.candidateOffsets, s.baseOffset+start)
	return true
}

//go:noinline
func (s *lexicographicRepMaxScan) considerBlock(
	data []byte,
	start int,
	maximumFirstByte byte,
) byte {
	firstBytes := data[start : start+8]
	for i, firstByte := range firstBytes {
		if maximumFirstByte <= firstByte && s.considerWindow(data, start+i) {
			maximumFirstByte = firstByte
		}
	}
	return maximumFirstByte
}

// scanLexicographicRepMaxRecordMaxima compares the fixed-size windows ending
// at successive cutting points. Most windows can be rejected from their first
// byte alone. The remaining prefix and suffix are read only when that byte
// could match or exceed the current maximum.
func scanLexicographicRepMaxRecordMaxima(
	data, maximumWindow []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	if len(maximumWindow) == 0 {
		panic("Missing maximum lexicographic window")
	}
	windowSize := len(maximumWindow)
	incomingLength := len(data) - windowSize

	scan := lexicographicRepMaxScan{
		candidateOffsets: frontier.candidateOffsets,
		maximumWindow:    maximumWindow,
		maximumPrefix:    frontier.maximumHash,
		baseOffset:       baseOffset,
	}
	maximumFirstByte := maximumWindow[0]
	candidateFirstBytes := data[1 : incomingLength+1]

	// The Go compiler does not unroll loops. Process eight bytes explicitly to
	// reduce loop-control work while keeping the rare full-window path separate.
	i := 0
	for ; i+8 <= len(candidateFirstBytes); i += 8 {
		b := candidateFirstBytes[i : i+8]
		if maximumFirstByte <= b[0] ||
			maximumFirstByte <= b[1] ||
			maximumFirstByte <= b[2] ||
			maximumFirstByte <= b[3] ||
			maximumFirstByte <= b[4] ||
			maximumFirstByte <= b[5] ||
			maximumFirstByte <= b[6] ||
			maximumFirstByte <= b[7] {
			maximumFirstByte = scan.considerBlock(
				data,
				i+1,
				maximumFirstByte,
			)
		}
	}
	for ; i < len(candidateFirstBytes); i++ {
		candidateFirstByte := candidateFirstBytes[i]
		if maximumFirstByte <= candidateFirstByte &&
			scan.considerWindow(data, i+1) {
			maximumFirstByte = candidateFirstByte
		}
	}

	frontier.candidateOffsets = scan.candidateOffsets
	frontier.currentHash = lexicographicWindowPrefix(data[len(data)-windowSize:])
	frontier.maximumHash = scan.maximumPrefix
}
