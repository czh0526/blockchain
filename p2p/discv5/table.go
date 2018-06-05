package discv5

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"sort"

	"github.com/czh0526/blockchain/common"
)

const (
	alpha      = 3
	bucketSize = 16
	hashBits   = len(common.Hash{}) * 8
	nBuckets   = hashBits + 1
)

type bucket struct {
	entries      []*Node
	replacements []*Node
}

type Table struct {
	count   int
	buckets [nBuckets]*bucket // Node 的二维数组
	self    *Node
}

func newTable(ourID NodeID, ourAddr *net.UDPAddr) *Table {
	self := NewNode(ourID, ourAddr.IP, uint16(ourAddr.Port), uint16(ourAddr.Port))
	tab := &Table{self: self}
	for i := range tab.buckets {
		tab.buckets[i] = new(bucket)
	}
	return tab
}

// 填充矢量距离表
func (tab *Table) stuff(nodes []*Node) {
	deleted := 0
outer:
	for _, n := range nodes {
		if n.ID == tab.self.ID {
			continue
		}
		// 定位到bucket
		logdist := logdist(tab.self.sha, n.sha)
		bucket := tab.buckets[logdist]
		// bucket内部排重
		for i := range bucket.entries {
			if bucket.entries[i].ID == n.ID {
				continue outer
			}
		}
		if len(bucket.entries) < bucketSize {
			bucket.entries = append(bucket.entries, n)
			tab.count++
		} else {
			deleted++
		}
	}
	if deleted > 0 {
		fmt.Printf("buckets已满，删除%d个 \n", deleted)
	}
}

// 以target为中心，取出nresults个距离target最近的节点
func (tab *Table) closest(target common.Hash, nresults int) *nodesByDistance {
	close := &nodesByDistance{target: target}
	for _, b := range tab.buckets {
		for _, n := range b.entries {
			close.push(n, nresults)
		}
	}
	return close
}

type nodesByDistance struct {
	entries []*Node
	target  common.Hash
}

// 将Node n 插入h内的对应位置
func (h *nodesByDistance) push(n *Node, maxElems int) {
	// 定位Node n应该插入的位置
	ix := sort.Search(len(h.entries), func(i int) bool {
		return distcmp(h.target, h.entries[i].sha, n.sha) > 0
	})
	if len(h.entries) < maxElems {
		h.entries = append(h.entries, n)
	}
	if ix == len(h.entries) {
		// 应该插入距离最远的位置
	} else {
		copy(h.entries[ix+1:], h.entries[ix:])
		h.entries[ix] = n
	}
}

func readUint(max uint32) uint32 {
	if max < 2 {
		return 0
	}
	var b [4]byte
	rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:]) % max
}
