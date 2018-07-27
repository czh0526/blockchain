package compiler

import (
	"os/exec"
	"testing"
)

const (
	testSource = `
contract test {
   /// @notice Will multiply ` + "`a`" + ` by 7.
   function multiply(uint a) returns(uint d) {
       return a * 7;
   }
}
`
)

func skipWithoutSolc(t *testing.T) {
	if _, err := exec.LookPath("solc"); err != nil {
		t.Skip(err)
	}
}

func TestCompiler(t *testing.T) {
	skipWithoutSolc(t)

	contracts, err := CompileSolidityString("", testSource)
	if err != nil {
		t.Fatalf("error compiling source. result %v: %v", contracts, err)
	}
	if len(contracts) != 1 {
		t.Errorf("one contract expected, got %d", len(contracts))
	}
	c, ok := contracts["test"]
	if !ok {
		c, ok = contracts["<stdin>:test"]
		if !ok {
			t.Fatal("info for contract 'test' not present in result")
		}
	}

	if c.Code == "" {
		t.Error("empty code")
	}

}
