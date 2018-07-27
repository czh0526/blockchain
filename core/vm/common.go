package vm

import (
	"math"
	"math/big"
)

func bigUint64(v *big.Int) (uint64, bool) {
	return v.Uint64(), v.BitLen() > 64
}

func toWordSize(size uint64) uint64 {
	// 溢出情况处理
	if size > math.MaxUint64-31 {
		return math.MaxUint64/32 + 1
	}
	// 正常情况处理
	return (size + 31) / 32
}
