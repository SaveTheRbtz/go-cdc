//go:build !amd64 || !goexperiment.simd

package cdc

func scanPolynomialRepMaxRecordMaxima(
	data []byte,
	baseOffset int,
	frontier *repMaxFrontier,
) {
	scanPolynomialRepMaxRecordMaximaScalar(data, baseOffset, frontier)
}
