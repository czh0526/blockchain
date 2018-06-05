package discv5

import (
	"crypto/ecdsa"
	"fmt"
	"math/rand"
	"net"
	"reflect"
	"testing"
	"testing/quick"
	"time"

	"github.com/czh0526/blockchain/common"
	"github.com/czh0526/blockchain/crypto"
)

type closeTest struct {
	Self   NodeID      // 自身节点
	Target common.Hash //
	All    []*Node     // 全网已知节点
	N      int
}

func (*closeTest) Generate(rand *rand.Rand, size int) reflect.Value {
	t := &closeTest{
		Self:   gen(NodeID{}, rand).(NodeID),
		Target: gen(common.Hash{}, rand).(common.Hash),
		N:      rand.Intn(bucketSize),
	}
	num := rand.Intn(100)
	for i := 0; i < num; i++ {
		id := gen(NodeID{}, rand).(NodeID)
		node := &Node{ID: id, sha: crypto.Keccak256Hash(id[:])}
		t.All = append(t.All, node)
	}
	return reflect.ValueOf(t)
}

func TestTable_closest(t *testing.T) {
	t.Parallel()
	test := func(test *closeTest) bool {
		fmt.Printf("\n构建一个Table, len = %d \n", len(test.All))
		// 以Self为中心，构建一个矢量距离表
		tab := newTable(test.Self, &net.UDPAddr{})
		// 将已知节点填入矢量距离表
		tab.stuff(test.All)

		fmt.Printf("  ==> ")
		for i, buckets := range tab.buckets {
			if len(buckets.entries) > 0 {
				fmt.Printf("%d(%d), ", i, len(buckets.entries))
			}
		}
		fmt.Println()

		// 取出距离Target最近的N个节点
		result := tab.closest(test.Target, test.N).entries
		// 检查结果是否有重复
		if hasDuplicates(result) {
			t.Errorf("result contains duplicates")
			return false
		}
		// 检查结果是否被排序
		if !sortedByDistanceTo(test.Target, result) {
			t.Errorf("result is not sorted by distance to target")
			return false
		}

		// 检查结果数量是否匹配
		wantN := test.N
		if tab.count < test.N {
			wantN = tab.count
		}
		if len(result) != wantN {
			t.Errorf("wrong number of nodes: got %d, want %d", len(result), wantN)
			return false
		} else if len(result) == 0 {
			return true
		}

		for _, b := range tab.buckets {
			for _, n := range b.entries {
				if contains(result, n.ID) {
					continue
				}
				farthestResult := result[len(result)-1].sha
				if distcmp(test.Target, n.sha, farthestResult) < 0 {
					t.Errorf("table contains node that is closer to target but it's not in result")
					t.Logf("  Target:          %v", test.Target)
					t.Logf("  Farthest Result: %v", farthestResult)
					t.Logf("  ID:              %v", n.ID)
					return false
				}
			}
		}
		return true
	}
	if err := quick.Check(test, quickcfg()); err != nil {
		t.Error(err)
	}
}

func TestTable_ReadRandomNodesGetAll(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 200,
		Rand:     rand.New(rand.NewSource(time.Now().Unix())),
		Values: func(args []reflect.Value, rand *rand.Rand) {
			args[0] = reflect.ValueOf(make([]*Node, rand.Intn(1000)))
		},
	}

	test := func(buf []*Node) bool {
		fmt.Printf("传递%d个Node", len(buf))
		// 构建一个空的table
		tab := newTable(NodeID{}, &net.UDPAddr{})
		for i := 0; i < len(buf); i++ {
			fmt.Println()
			// 随机设定一个逻辑距离
			ld := cfg.Rand.Intn(len(tab.buckets))
			fmt.Printf("逻辑距离 ==> %d\n", ld)
			// 构造一个节点填充table
			node := nodeAtDistance(tab.self.sha, ld)
			fmt.Printf("中心节点 ==> 0x%x \n", tab.self.sha[:])
			fmt.Printf("构造节点 ==> 0x%x \n", node.sha[:])
			tab.stuff([]*Node{node})
		}

		gotN := tab.readRandomNodes(buf)
		if gotN != tab.count {
			t.Errorf("wrong number of nodes, got %d, want %d", gotN, tab.count)
			return false
		}
		if hasDuplicates(buf[:gotN]) {
			t.Errorf("result contains duplicates")
			return false
		}

		return true
	}

	if err := quick.Check(test, cfg); err != nil {
		t.Error(err)
	}
}

func (tab *Table) readRandomNodes(buf []*Node) (n int) {
	var buckets [][]*Node
	for _, b := range tab.buckets {
		if len(b.entries) > 0 {
			buckets = append(buckets, b.entries[:])
		}
	}
	if len(buckets) == 0 {
		return 0
	}

	for i := uint32(len(buckets)) - 1; i > 0; i-- {
		j := readUint(i)
		buckets[i], buckets[j] = buckets[j], buckets[i]
	}

	var i int // buf的游标
	var j int // buckets的游标
	for ; i < len(buf); i, j = i+1, (j+1)%len(buckets) {
		b := buckets[j]
		buf[i] = &(*b[0])
		buckets[j] = b[1:]
		if len(b) == 1 {
			buckets = append(buckets[:j], buckets[j+1:]...)
		}
		if len(buckets) == 0 {
			break
		}
	}
	return i + 1
}

func newkey() *ecdsa.PrivateKey {
	key, err := crypto.GenerateKey()
	if err != nil {
		panic("couldn't generate key: " + err.Error())
	}
	return key
}

func gen(typ interface{}, rand *rand.Rand) interface{} {
	v, ok := quick.Value(reflect.TypeOf(typ), rand)
	if !ok {
		panic(fmt.Sprintf("couldn't generate random value of type %T", typ))
	}
	return v.Interface()
}

// 构建一个testing/quick包的Config对象
func quickcfg() *quick.Config {
	return &quick.Config{
		MaxCount: 50,
		Rand:     rand.New(rand.NewSource(time.Now().Unix())),
	}
}

func hasDuplicates(slice []*Node) bool {
	seen := make(map[NodeID]bool)
	for i, e := range slice {
		if e == nil {
			panic(fmt.Sprintf("nil *Node at %d", i))
		}
		if seen[e.ID] {
			return true
		}
		seen[e.ID] = true
	}
	return false
}

func sortedByDistanceTo(distbase common.Hash, slice []*Node) bool {
	var last common.Hash
	for i, e := range slice {
		if i > 0 && distcmp(distbase, e.sha, last) < 0 {
			return false
		}
		last = e.sha
	}
	return true
}

func contains(ns []*Node, id NodeID) bool {
	for _, n := range ns {
		if n.ID == id {
			return true
		}
	}
	return false
}

func nodeAtDistance(base common.Hash, ld int) (n *Node) {
	n = new(Node)
	n.sha = hashAtDistance(base, ld)
	copy(n.ID[:], n.sha[:])
	return n
}
