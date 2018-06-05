package discover

import (
	"bytes"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
)

const Version = 4

var nodeDBKeyTests = []struct {
	id    NodeID
	field string
	key   []byte
}{
	{
		id:    NodeID{},
		field: "version",
		key:   []byte{0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e},
	},
	{
		id:    MustHexID("0x1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439"),
		field: ":discover",
		key: []byte{0x6e, 0x3a, // prefix
			0x1d, 0xd9, 0xd6, 0x5c, 0x45, 0x52, 0xb5, 0xeb, // node id
			0x43, 0xd5, 0xad, 0x55, 0xa2, 0xee, 0x3f, 0x56, //
			0xc6, 0xcb, 0xc1, 0xc6, 0x4a, 0x5c, 0x8d, 0x65, //
			0x9f, 0x51, 0xfc, 0xd5, 0x1b, 0xac, 0xe2, 0x43, //
			0x51, 0x23, 0x2b, 0x8d, 0x78, 0x21, 0x61, 0x7d, //
			0x2b, 0x29, 0xb5, 0x4b, 0x81, 0xcd, 0xef, 0xb9, //
			0xb3, 0xe9, 0xc3, 0x7d, 0x7f, 0xd5, 0xf6, 0x32, //
			0x70, 0xbc, 0xc9, 0xe1, 0xa6, 0xf6, 0xa4, 0x39, //
			0x3a, 0x64, 0x69, 0x73, 0x63, 0x6f, 0x76, 0x65, 0x72, // field
		},
	},
}

func TestNodeDBKeys(t *testing.T) {
	for i, tt := range nodeDBKeyTests {
		key := makeKey(tt.id, tt.field)
		fmt.Printf("id = 0x%x, field = %s, key = %s \n", tt.id[:], tt.field, tt.key)
		if !bytes.Equal(key, tt.key) {
			t.Errorf("make test %d: key mismatch: have 0x%x, want 0x%x", i, key, tt.key)
		}
		id, field := splitKey(tt.key)
		fmt.Printf("id = 0x%x, field = %s \n", id, field)
		if !bytes.Equal(id[:], tt.id[:]) {
			t.Errorf("split test %d: id mismatch: jave 0x%x, want 0x%x", i, id, tt.id)
		}
		if field != tt.field {
			t.Errorf("split test %d: field mismatch: have 0x%x, want 0x%x", i, field, tt.field)
		}
		fmt.Println()
	}
}

var nodeDBInt64Tests = []struct {
	key   []byte
	value int64
}{
	{key: []byte{0x01}, value: 1},
	{key: []byte{0x02}, value: 2},
	{key: []byte{0x03}, value: 3},
}

func TestNodeDBInt64(t *testing.T) {
	db, _ := newNodeDB("", Version, NodeID{})
	defer db.close()

	tests := nodeDBInt64Tests
	for i := 0; i < len(tests); i++ {
		if err := db.storeInt64(tests[i].key, tests[i].value); err != nil {
			t.Errorf("test %d: failed to store value: %v", i, err)
		}

		for j := 0; j < len(tests); j++ {
			num := db.fetchInt64(tests[j].key)
			switch {
			case j <= i && num != tests[j].value:
				t.Errorf("test %d, item %d: value mismatch: have %v, want %v", i, j, num, tests[j].value)
			case j > i && num != 0:
				t.Errorf("test %d, item %d: value mismatch: have %v, want %v.", i, j, num, 0)
			}
		}
	}
}

func TestNodeDBFetchStore(t *testing.T) {
	node := NewNode(
		MustHexID("0x1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439"),
		net.IP{192, 168, 0, 1},
		30303,
		30303,
	)
	inst := time.Now()
	num := 314

	db, _ := newNodeDB("", Version, NodeID{})
	defer db.close()

	if stored := db.lastPing(node.ID); stored.Unix() != 0 {
		t.Errorf("ping: non-existing object: %v", stored)
	}

	if err := db.updateLastPing(node.ID, inst); err != nil {
		t.Errorf("ping: failed to update: %v", err)
	}
	if stored := db.lastPing(node.ID); stored.Unix() != inst.Unix() {
		t.Errorf("ping: value mismatch: have %v, want %v", stored, inst)
	}

	if stored := db.bondTime(node.ID); stored.Unix() != 0 {
		t.Errorf("ping: non-existing object: %v", stored)
	}
	if err := db.updateBondTime(node.ID, inst); err != nil {
		t.Errorf("pong: failed to udpate: %v", err)
	}
	if stored := db.bondTime(node.ID); stored.Unix() != inst.Unix() {
		t.Errorf("pong: value mismatch: have %v, want %v", stored, inst)
	}

	if stored := db.findFails(node.ID); stored != 0 {
		t.Errorf("find-node failed: non-existing object: %v", stored)
	}
	if err := db.updateFindFails(node.ID, num); err != nil {
		t.Errorf("find-node fails: failed to update: %v", err)
	}
	if stored := db.findFails(node.ID); stored != num {
		t.Errorf("find-node fails: value mismatch: have %v, want %v", stored, num)
	}
	if stored := db.node(node.ID); stored != nil {
		t.Errorf("node: non-existing object: %v", stored)
	}
	if err := db.updateNode(node); err != nil {
		t.Errorf("node: failed to update: %v", err)
	}
	if stored := db.node(node.ID); stored == nil {
		t.Errorf("node: not found")
	} else if !reflect.DeepEqual(stored, node) {
		t.Errorf("node: data mismatch: have %v, want %v", stored, node)
	}
}
