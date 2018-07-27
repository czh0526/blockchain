package vm

import (
	"fmt"
	"math/big"
)

type Stack struct {
	data []*big.Int
}

func newstack() *Stack {
	return &Stack{data: make([]*big.Int, 0, 1024)}
}

func (st *Stack) Data() []*big.Int {
	return st.data
}

func (st *Stack) push(d *big.Int) {
	st.data = append(st.data, d)
}

func (st *Stack) pushN(ds ...*big.Int) {
	st.data = append(st.data, ds...)
}

func (st *Stack) pop() (ret *big.Int) {
	ret = st.data[len(st.data)-1]
	st.data = st.data[:len(st.data)-1]
	return
}

func (st *Stack) len() int {
	return len(st.data)
}

// 把栈顶元素和位置 n 的元素互换
func (st *Stack) swap(n int) {
	st.data[st.len()-n], st.data[st.len()-1] = st.data[st.len()-1], st.data[st.len()-n]
}

// 将位置 n 的元素复制到 intPool 中
func (st *Stack) dup(pool *intPool, n int) {
	st.push(pool.get().Set(st.data[st.len()-n]))
}

func (st *Stack) peek() *big.Int {
	return st.data[st.len()-1]
}

// 读取从栈顶开始的第 n 个位置的元素
func (st *Stack) Back(n int) *big.Int {
	return st.data[st.len()-n-1]
}

func (st *Stack) require(n int) error {
	if st.len() < n {
		return fmt.Errorf("stack underflow (%d <=> %d)", len(st.data), n)
	}
	return nil
}
