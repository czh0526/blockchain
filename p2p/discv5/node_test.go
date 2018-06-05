package discv5

import (
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/czh0526/blockchain/crypto"
)

var parseNodeTests = []struct {
	rawurl     string
	wantError  string
	wantResult *Node
}{
	{
		rawurl:    "http://foobar",
		wantError: `invalid URL scheme, want "enode"`,
	},
	{
		rawurl:    "enode://01010101@123.124.125.126:3",
		wantError: `invalid node ID (wrong length, want 128 hex chars)`,
	},
	{
		rawurl:    "enode://1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439@hostname:3",
		wantError: `invalid IP address`,
	},
	{
		rawurl:    "enode://1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439@127.0.0.1:3?discport=foo",
		wantError: `invalid discport in query`,
	},
	{
		rawurl: "enode://1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439@127.0.0.1:52150",
		wantResult: NewNode(
			MustHexID("0x1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439"),
			net.IP{0x7f, 0x0, 0x0, 0x1},
			52150,
			52150,
		),
	},
	{
		rawurl: "enode://1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439@[::]:52150",
		wantResult: NewNode(
			MustHexID("0x1dd9d65c4552b5eb43d5ad55a2ee3f56c6cbc1c64a5c8d659f51fcd51bace24351232b8d7821617d2b29b54b81cdefb9b3e9c37d7fd5f63270bcc9e1a6f6a439"),
			net.ParseIP("::"),
			52150,
			52150,
		),
	},
}

func TestParseNode(t *testing.T) {
	for _, test := range parseNodeTests {
		n, err := ParseNode(test.rawurl)
		if test.wantError != "" {
			if err == nil {
				t.Errorf("test %q:\n got nil error, expected %#q", test.rawurl, test.wantError)
				continue
			} else if err.Error() != test.wantError {
				t.Errorf("test %q:\n got error %#q, expected %#q", test.rawurl, err.Error(), test.wantError)
				continue
			}
		} else {
			if err != nil {
				t.Errorf("test %q\n unexpected error: %v", test.rawurl, err)
				continue
			}
			if !reflect.DeepEqual(n, test.wantResult) {
				t.Errorf("test %q:\n result mismatch:\n got: %#v, want: %#v", test.rawurl, n, test.wantResult)
			}
		}
	}
}

func TestNodeString(t *testing.T) {
	for i, test := range parseNodeTests {
		if test.wantError == "" && strings.HasPrefix(test.rawurl, "enode://") {
			str := test.wantResult.String()
			if str != test.rawurl {
				t.Errorf("test %d: Node.String() mismatch:\ngot: %s\nwant: %s", i, str, test.rawurl)
			}
		}
	}
}

func TestNodeID_recover(t *testing.T) {
	// 生成一个密钥
	prv := newkey()
	// 生成一个随机的数据Hash
	hash := make([]byte, 32)
	// 对Hash做签名
	sig, err := crypto.Sign(hash, prv)
	if err != nil {
		t.Fatalf("signing error: %v", err)
	}

	// 根据公钥生成 NodeID
	pub := PubkeyID(&prv.PublicKey)
	// 根据 Hash 和 签名恢复NodeID
	recpub, err := recoverNodeID(hash, sig)
	if err != nil {
		t.Fatalf("recovery error: %v", err)
	}
	// 比较根据公钥生成的 NodeID 和根据Hash和sig恢复的NodeID
	if pub != recpub {
		t.Errorf("recovered wrong pubkey:\ngot: %v\nwant: %v", recpub, pub)
	}

	// 根据NodeID恢复公钥
	ecdsa, err := pub.Pubkey()
	if err != nil {
		t.Errorf("Pubkey error: %v", err)
	}
	// 比较根据NodeID恢复的公钥和原始公钥是否一致
	if !reflect.DeepEqual(ecdsa, &prv.PublicKey) {
		t.Errorf("Pubkey mismatch:\n got: %#v\n want: %#v", ecdsa, &prv.PublicKey)
	}
}
