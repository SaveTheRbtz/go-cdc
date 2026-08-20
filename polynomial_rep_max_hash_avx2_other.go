//go:build !amd64 || purego

package cdc

func newPolynomialAVX2RepMaxHash() repMaxHash {
	panic("AVX2 polynomial RepMaxCDC is unavailable in purego or non-amd64 builds")
}

func scanPolynomialRepMaxRecordMaximaAVX2(
	data []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	panic("AVX2 polynomial RepMaxCDC is unavailable in purego or non-amd64 builds")
}
