package vm

import (
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/params"
)

func TestBigInt(t *testing.T) {
	arg1 := "100000000000000000000000000000000000000000000000000000000000000000000001"
	arg2 := "10000000000000000000000000000000000000000000000000000000000000000000001"

	x := new(big.Int).SetBytes(common.Hex2Bytes(arg1))
	y := new(big.Int).SetBytes(common.Hex2Bytes(arg2))

	z := x.Add(x, y)
	fmt.Printf("x := %x \n", x)
	fmt.Printf("z := %x \n", z)
}

func TestByteOp(t *testing.T) {
	var (
		env   = NewEVM(Context{}, nil, params.TestChainConfig, Config{})
		stack = newstack()
	)
	tests := []struct {
		v        string
		th       uint64
		expected *big.Int
	}{
		{"ABCDEF0908070605040302010000000000000000000000000000000000000000", 0, big.NewInt(0xAB)},
		{"ABCDEF0908070605040302010000000000000000000000000000000000000000", 1, big.NewInt(0xCD)},
	}

	pc := uint64(0)
	for _, test := range tests {
		val := new(big.Int).SetBytes(common.Hex2Bytes(test.v))
		th := new(big.Int).SetUint64(test.th)
		stack.push(val)
		stack.push(th)
		opByte(&pc, env, nil, nil, stack)
		actual := stack.pop()
		if actual.Cmp(test.expected) != 0 {
			t.Fatalf("Expected [%v] %v:th byte to be %v, was %v.", test.v, test.th, test.expected, actual)
		}
	}
}

func testOp2(t *testing.T, op func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error), expected string, args ...string) {
	var (
		env   = NewEVM(Context{}, nil, params.TestChainConfig, Config{})
		stack = newstack()
	)

	byteArgs := make([][]byte, len(args))
	for i, arg := range args {
		byteArgs[i] = common.Hex2Bytes(arg)
	}
	pc := uint64(0)
	for _, arg := range byteArgs {
		a := new(big.Int).SetBytes(arg)
		stack.push(a)
	}

	op(&pc, env, nil, nil, stack)
	actual := stack.pop()
	if strings.Compare(expected, common.Bytes2Hex(actual.Bytes())) != 0 {
		t.Fatalf("expected %s, got %x", expected, actual)
	}
}

func TestOpAdd64(t *testing.T) {
	x := "AAAAAAAAAAAAAAAA" // 64位
	y := "9999999999999999" // 64位
	expected := "014444444444444443"

	testOp2(t, opAdd, expected, x, y)
}

func TestOpAdd128(t *testing.T) {
	x := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 128位
	y := "99999999999999999999999999999999" // 128位
	expected := "0144444444444444444444444444444443"

	testOp2(t, opAdd, expected, x, y)
}

func TestOpAdd256(t *testing.T) {
	x := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	y := "9999999999999999999999999999999999999999999999999999999999999999"

	expected := "4444444444444444444444444444444444444444444444444444444444444443"

	testOp2(t, opAdd, expected, x, y)
}
