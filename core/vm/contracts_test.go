package vm

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/czh0526/blockchain/common"
)

type precompiledTest struct {
	input, expected string
	gas             uint64
	name            string
	noBenchmark     bool
}

var bn256AddTests = []precompiledTest{
	{
		input:    "18b18acfb4c2c30276db5411368e7185b311dd124691610c5d3b74034e093dc9063c909c4720840cb5134cb9f59fa749755796819658d32efc0d288198f3726607c2b7f58a84bd6145f00c9c2bc0bb1a187f20ff2c92963a88019e7c6a014eed06614e20c147e940f2d70da3f74c9a17df361706a4485c742bd6788478fa17d7",
		expected: "2243525c5efd4b9c3d3c45ac0ca3fe4dd85e830a4ce6b65fa1eeaee202839703301d1d33be6da8e509df21cc35964723180eed7532537db9ae5e7d48f195c915",
		name:     "chfast1",
	}, {
		input:    "2243525c5efd4b9c3d3c45ac0ca3fe4dd85e830a4ce6b65fa1eeaee202839703301d1d33be6da8e509df21cc35964723180eed7532537db9ae5e7d48f195c91518b18acfb4c2c30276db5411368e7185b311dd124691610c5d3b74034e093dc9063c909c4720840cb5134cb9f59fa749755796819658d32efc0d288198f37266",
		expected: "2bd3e6d0f3b142924f5ca7b49ce5b9d54c4703d7ae5648e61d02268b1a0a9fb721611ce0a6af85915e2f1d70300909ce2e49dfad4a4619c8390cae66cefdb204",
		name:     "chfast2",
	}, {
		input:    "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		expected: "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		name:     "cdetrio1",
	}, {
		input:    "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		expected: "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		name:     "cdetrio2",
	},
}

var modexpTests = []precompiledTest{
	{
		input: "0000000000000000000000000000000000000000000000000000000000000001" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"03" +
			"fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2e" +
			"fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2f",
		expected: "0000000000000000000000000000000000000000000000000000000000000001",
		name:     "eip_example1",
	}, {
		input: "0000000000000000000000000000000000000000000000000000000000000000" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"0000000000000000000000000000000000000000000000000000000000000020" +
			"fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2e" +
			"fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2f",
		expected: "0000000000000000000000000000000000000000000000000000000000000000",
		name:     "eip_example2",
	},
}

/*
 	addr: 预编译的合约地址
	test：测试数据包
	bench: 测试工具
*/
func benchmarkPrecompiled(addr string, test precompiledTest, bench *testing.B) {
	if test.noBenchmark {
		return
	}
	// 获取预编译合约
	p := PrecompiledContractsHomestead[common.HexToAddress(addr)]
	// 计算本次需要消耗的 gas
	in := common.Hex2Bytes(test.input)
	reqGas := p.RequiredGas(in)
	// 构建合约实例
	contract := NewContract(AccountRef(common.HexToAddress("1337")),
		nil, new(big.Int), reqGas)

	var (
		res  []byte
		err  error
		data = make([]byte, len(in))
	)

	bench.Run(fmt.Sprintf("%s-Gas=%d", test.name, contract.Gas), func(bench *testing.B) {
		bench.ResetTimer()
		for i := 0; i < bench.N; i++ {
			contract.Gas = reqGas
			copy(data, in)
			// 通过 contract 调用预编译合约
			res, err = RunPrecompiledContract(p, data, contract)
		}
		bench.StopTimer()
		if err != nil {
			bench.Error(err)
			return
		}
		if common.Bytes2Hex(res) != test.expected {
			bench.Error(fmt.Sprintf("Expected %v, got %x", test.expected, common.Bytes2Hex(res)))
		}
	})
}

func BenchmarkPrecompiledEcrecover(bench *testing.B) {
	t := precompiledTest{
		input:    "38d18acb67d25c8bb9942764b62f18e17054f66a817bd4295423adf9ed98873e000000000000000000000000000000000000000000000000000000000000001b38d18acb67d25c8bb9942764b62f18e17054f66a817bd4295423adf9ed98873e789d1dd423d25f0772d2748d60f7e4b81bb14d086eba8e8e8efb6dcff8a4ae02",
		expected: "000000000000000000000000ceaccac640adf55b2028469bd36ba501f28b699d",
		name:     "ecrecover-example",
	}
	benchmarkPrecompiled("01", t, bench)
}
