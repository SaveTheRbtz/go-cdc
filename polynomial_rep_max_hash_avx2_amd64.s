//go:build amd64 && !purego

#include "textflag.h"

// These constants mirror polynomialHashRemovalFactor and
// polynomialHashRollingAdjustment in polynomial_hash.go. The outgoing byte is
// at most 255, so multiplying it by the removal factor only needs the low-low
// and low-high products.
DATA polynomialAVX2RemovalLow<>+0(SB)/8, $0x000000009c3b3301
DATA polynomialAVX2RemovalLow<>+8(SB)/8, $0x000000009c3b3301
DATA polynomialAVX2RemovalLow<>+16(SB)/8, $0x000000009c3b3301
DATA polynomialAVX2RemovalLow<>+24(SB)/8, $0x000000009c3b3301
GLOBL polynomialAVX2RemovalLow<>(SB), RODATA|NOPTR, $32

DATA polynomialAVX2RemovalHigh<>+0(SB)/8, $0x0000000066c0333b
DATA polynomialAVX2RemovalHigh<>+8(SB)/8, $0x0000000066c0333b
DATA polynomialAVX2RemovalHigh<>+16(SB)/8, $0x0000000066c0333b
DATA polynomialAVX2RemovalHigh<>+24(SB)/8, $0x0000000066c0333b
GLOBL polynomialAVX2RemovalHigh<>(SB), RODATA|NOPTR, $32

DATA polynomialAVX2Adjustment<>+0(SB)/8, $0x993fccc463c4cd00
DATA polynomialAVX2Adjustment<>+8(SB)/8, $0x993fccc463c4cd00
DATA polynomialAVX2Adjustment<>+16(SB)/8, $0x993fccc463c4cd00
DATA polynomialAVX2Adjustment<>+24(SB)/8, $0x993fccc463c4cd00
GLOBL polynomialAVX2Adjustment<>(SB), RODATA|NOPTR, $32

// func scanPolynomialRepMaxRecordMaximaAVX2Core(
//     data []byte,
//     currentHash uint64,
//     maximumHash uint64,
//     candidateOffsets *[32]int,
// ) (bytesScanned, candidateCount int, nextHash, nextMaximum uint64)
TEXT ·scanPolynomialRepMaxRecordMaximaAVX2Core(SB), NOSPLIT, $32-80
	MOVQ data_base+0(FP), SI
	MOVQ data_len+8(FP), CX
	SUBQ $64, CX
	MOVQ currentHash+24(FP), AX
	MOVQ maximumHash+32(FP), DX
	MOVQ candidateOffsets+40(FP), R9
	XORQ R8, R8
	XORQ R10, R10

	VMOVDQU polynomialAVX2RemovalLow<>+0(SB), Y12
	VMOVDQU polynomialAVX2RemovalHigh<>+0(SB), Y13
	MOVQ $0x9e3779b97f4a7c15, R11

	PCALIGN $32
avx2_loop:
	CMPQ R8, CX
	JAE avx2_done
	// Reserve four output slots before processing a block. This lets the
	// wrapper resume at a block boundary even for adversarial input where
	// every hash is a new record.
	CMPQ R10, $29 // polynomialAVX2CandidateBufferSize - 4 + 1
	JAE avx2_done

	VPMOVZXBQ 64(SI)(R8*1), Y0
	VPMOVZXBQ (SI)(R8*1), Y1

	// delta[i] = incoming[i] - outgoing[i]*removalFactor + adjustment.
	VPMULUDQ Y12, Y1, Y2
	VPMULUDQ Y13, Y1, Y3
	VPSLLQ $32, Y3, Y3
	VPADDQ Y3, Y2, Y2
	VPADDQ polynomialAVX2Adjustment<>+0(SB), Y0, Y0
	VPSUBQ Y2, Y0, Y0

	// Apply the four adjustments to the rolling hash in scalar order,
	// preserving the exact recurrence and its record maxima.
	VMOVDQU Y0, 0(SP)

	// CMPQ/JAE is unsigned and skips equal hashes, preserving the scalar
	// implementation's leftmost tie behavior.
	IMULQ R11, AX
	ADDQ 0(SP), AX
	CMPQ DX, AX
	JAE lane1
	MOVQ AX, DX
	LEAQ 1(R8), R12
	MOVQ R12, (R9)(R10*8)
	INCQ R10

lane1:
	IMULQ R11, AX
	ADDQ 8(SP), AX
	CMPQ DX, AX
	JAE lane2
	MOVQ AX, DX
	LEAQ 2(R8), R12
	MOVQ R12, (R9)(R10*8)
	INCQ R10

lane2:
	IMULQ R11, AX
	ADDQ 16(SP), AX
	CMPQ DX, AX
	JAE lane3
	MOVQ AX, DX
	LEAQ 3(R8), R12
	MOVQ R12, (R9)(R10*8)
	INCQ R10

lane3:
	IMULQ R11, AX
	ADDQ 24(SP), AX
	CMPQ DX, AX
	JAE block_done
	MOVQ AX, DX
	LEAQ 4(R8), R12
	MOVQ R12, (R9)(R10*8)
	INCQ R10

block_done:
	ADDQ $4, R8
	JMP avx2_loop

avx2_done:
	MOVQ R8, bytesScanned+48(FP)
	MOVQ R10, candidateCount+56(FP)
	MOVQ AX, nextHash+64(FP)
	MOVQ DX, nextMaximum+72(FP)
	VZEROUPPER
	RET
