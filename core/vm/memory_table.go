package vm

import (
	"math/big"

	"github.com/czh0526/blockchain/common/math"
)

func memorySha3(stack *Stack) *big.Int {
	return calcMemSize(stack.Back(0), stack.Back(1))
}

// 8 bits / 1 bytes
func memoryMStore8(stack *Stack) *big.Int {
	return calcMemSize(stack.Back(0), big.NewInt(1))
}

// 256 bits / 32 bytes
func memoryMStore(stack *Stack) *big.Int {
	return calcMemSize(stack.Back(0), big.NewInt(32))
}

func memoryMLoad(stack *Stack) *big.Int {
	return calcMemSize(stack.Back(0), big.NewInt(32))
}

func memoryReturn(stack *Stack) *big.Int {
	return calcMemSize(stack.Back(0), stack.Back(1))
}

func memoryCallDataCopy(stack *Stack) *big.Int {
	return calcMemSize(stack.Back(0), stack.Back(2))
}

func memoryCallCode(stack *Stack) *big.Int {
	// ret memory
	x := calcMemSize(stack.Back(5), stack.Back(6))
	// in-params memory
	y := calcMemSize(stack.Back(3), stack.Back(4))

	return math.BigMax(x, y)
}
