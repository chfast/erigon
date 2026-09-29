// Copyright 2026 The Erigon Authors
// This file is part of Erigon.
//
// Erigon is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Erigon is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with Erigon. If not, see <http://www.gnu.org/licenses/>.

#include "textflag.h"

// Branch-free JUMPDEST analysis of 32-byte blocks (NEON).
//
// Each 8-byte half of a 16-byte group is solved for every entry offset at once: next[i] = i + 1 +
// pushlen, followed by 3 pointer-jumping rounds with TBL (v |= v[x], x = x[x]). A pointer that
// leaves its half points to itself. v collects the JUMPDESTs visited from each entry. The entry of
// the next group is kept in a vector register: one TBX per group, which leaves entries past the
// group unchanged. Small tables, indexed by the entry offset, give the JUMPDEST bits of the real
// entry. The cost does not depend on the code bytes.
//
// Table layout (tab, 224 bytes, zeroed by the caller; only the first 16 bytes of each are written):
//   T0   0: exit of the 1st half of group A (0..48), by entry 0..7
//   T2  48: the same for group C
//   VA  96: JUMPDEST bits of the half of group A, by entry
//   VC 160: the same for group C

DATA jdnIota<>+0(SB)/8, $0xa9a8a7a6a5a4a3a2 // lane + 1 - 0x5f
DATA jdnIota<>+8(SB)/8, $0xb1b0afaeadacabaa
GLOBL jdnIota<>(SB), RODATA|NOPTR, $16
DATA jdnHalfEnd<>+0(SB)/8, $0x0808080808080808
DATA jdnHalfEnd<>+8(SB)/8, $0x1010101010101010
GLOBL jdnHalfEnd<>(SB), RODATA|NOPTR, $16
DATA jdnLane<>+0(SB)/8, $0x0706050403020100
DATA jdnLane<>+8(SB)/8, $0x0f0e0d0c0b0a0908
GLOBL jdnLane<>(SB), RODATA|NOPTR, $16
DATA jdnBit<>+0(SB)/8, $0x8040201008040201
DATA jdnBit<>+8(SB)/8, $0x8040201008040201
GLOBL jdnBit<>(SB), RODATA|NOPTR, $16

// GROUP computes, for the 16 code bytes in c, per entry lane: ex = the exit of the lane's half,
// xm = the exit of the group (16 + entry into the next group), v = the JUMPDESTs visited.
// smaxw and cmhiw encode "smax n, c, V20" and "cmhi x, V17, n", missing in the Go 1.26 assembler.
// The words hard-code the registers: keep them in sync with the GROUP arguments.
#define GROUP(c, n, x, v, ex, xm, t, smaxw, cmhiw) \
	WORD  $smaxw \                   // n = max_s8(c, 0x5f)
	VADD  V16.B16, n.B16, n.B16 \    // n = next instruction start
	WORD  $cmhiw \                   // x = n < half end
	VBSL  V18.B16, n.B16, x.B16 \    // x = next start in the half, or the lane itself (a sink)
	VCMEQ V21.B16, c.B16, v.B16 \
	VAND  V19.B16, v.B16, v.B16 \
	VTBL  x.B16, [v.B16], t.B16 \
	VORR  t.B16, v.B16, v.B16 \
	VTBL  x.B16, [x.B16], x.B16 \
	VTBL  x.B16, [v.B16], t.B16 \
	VORR  t.B16, v.B16, v.B16 \
	VTBL  x.B16, [x.B16], x.B16 \
	VTBL  x.B16, [v.B16], t.B16 \
	VORR  t.B16, v.B16, v.B16 \
	VTBL  x.B16, [x.B16], x.B16 \
	VTBL  x.B16, [n.B16], ex.B16 \   // exit of the half
	VTBL  ex.B16, [ex.B16], t.B16 \
	VUMAX t.B16, ex.B16, xm.B16      // 1st-half exits inside the 2nd half take its exit

// func jumpdestBitmapNEON(code *byte, blocks int, tab *byte, out *uint64) (entry int)
TEXT ·jumpdestBitmapNEON(SB), NOSPLIT, $0-40
	MOVD code+0(FP), R0
	MOVD blocks+8(FP), R1
	MOVD tab+16(FP), R2
	MOVD out+24(FP), R3
	MOVD $jdnIota<>(SB), R4
	VLD1 (R4), [V16.B16]
	MOVD $jdnHalfEnd<>(SB), R4
	VLD1 (R4), [V17.B16]
	MOVD $jdnLane<>(SB), R4
	VLD1 (R4), [V18.B16]
	MOVD $jdnBit<>(SB), R4
	VLD1 (R4), [V19.B16]
	VMOVI $0x5f, V20.B16
	VMOVI $0x5b, V21.B16
	VMOVI $16, V22.B16
	VEOR  V30.B16, V30.B16, V30.B16 // e: the block entry in every lane
	ADD   $48, R2, R5               // T2
	ADD   $96, R2, R6               // VA
	ADD   $160, R2, R7              // VC

loop:
	VLD1.P 32(R0), [V0.B16, V1.B16]
	GROUP(V0, V6, V7, V4, V2, V3, V5, 0x4e346406, 0x6e263627)
	GROUP(V1, V12, V13, V10, V8, V9, V11, 0x4e34642c, 0x6e2c362d)
	VST1  [V2.B16], (R2)
	VST1  [V8.B16], (R5)
	VST1  [V4.B16], (R6)
	VST1  [V10.B16], (R7)

	// e2 = entry into group C, then e = entry into the next block.
	VORR  V30.B16, V30.B16, V14.B16
	VTBX  V30.B16, [V3.B16], V14.B16
	VSUB  V22.B16, V14.B16, V14.B16
	VMOV  V30.B[0], R8              // s
	VMOV  V14.B[0], R9              // s2
	VORR  V14.B16, V14.B16, V30.B16
	VTBX  V14.B16, [V9.B16], V30.B16
	VSUB  V22.B16, V30.B16, V30.B16

	MOVBU (R2)(R8), R10             // T0[s]
	MOVBU (R5)(R9), R11             // T2[s2]
	MOVBU (R6)(R8), R12             // VA[s]
	MOVBU (R7)(R9), R13             // VC[s2]
	CMP   $8, R8
	CSEL  LO, R10, R8, R10          // e1: entry into the 2nd half of A
	CSEL  LO, R12, ZR, R12
	CMP   $8, R9
	CSEL  LO, R11, R9, R11          // e3
	CSEL  LO, R13, ZR, R13
	MOVBU (R6)(R10), R10            // VA[e1]
	MOVBU (R7)(R11), R11            // VC[e3]
	ORR   R10<<8, R12, R12
	ORR   R13<<16, R12, R12
	ORR   R11<<24, R12, R12
	MOVW  R12, (R3)
	ADD   $4, R3
	SUBS  $1, R1
	BNE   loop

	VMOV V30.B[0], R8
	MOVD R8, entry+32(FP)
	RET
