// +build amd64

#include "textflag.h"

// func distSqAVX2(vecs, query unsafe.Pointer) int64
//
// vecs   -> pointer to a packed int16[14] (the 14-dim reference vector)
// query  -> pointer to a padded int16[16] (last two lanes must be zero)
//
// Strategy:
//   load 32 bytes from vecs (lanes 14,15 may be garbage from the next row)
//   load 32 bytes from query (lanes 14,15 are 0 by contract)
//   subtract → 16 int16 diffs (lanes 14,15 of the diff are still garbage)
//   AND with a mask that zeros lanes 14,15
//   VPMADDWD squares each lane and sums adjacent pairs → 8 int32 sums
//   horizontal-add the 8 int32 lanes to a single int32
//   return as int64
TEXT ·distSqAVX2(SB), NOSPLIT, $0-24
	MOVQ vecs+0(FP), DI
	MOVQ query+8(FP), SI

	VMOVDQU (DI), Y0
	VMOVDQU (SI), Y1
	VPSUBW  Y1, Y0, Y0

	VMOVDQU ·distMask(SB), Y2
	VPAND   Y2, Y0, Y0

	VPMADDWD Y0, Y0, Y0

	VEXTRACTI128 $1, Y0, X1
	VPADDD       X1, X0, X0
	VPSHUFD      $0xEE, X0, X1
	VPADDD       X1, X0, X0
	VPSHUFD      $0xE1, X0, X1
	VPADDD       X1, X0, X0

	VMOVD     X0, AX
	MOVLQSX   AX, AX
	VZEROUPPER

	MOVQ AX, ret+16(FP)
	RET

// distMask zeros the last two int16 lanes of the YMM diff so the
// out-of-row reads from the 32-byte load don't pollute the sum.
DATA ·distMask+0(SB)/8,  $0xFFFFFFFFFFFFFFFF
DATA ·distMask+8(SB)/8,  $0xFFFFFFFFFFFFFFFF
DATA ·distMask+16(SB)/8, $0xFFFFFFFFFFFFFFFF
DATA ·distMask+24(SB)/4, $0xFFFFFFFF
DATA ·distMask+28(SB)/4, $0x00000000
GLOBL ·distMask(SB), RODATA|NOPTR, $32
