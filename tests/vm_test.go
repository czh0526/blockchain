package tests

import (
	"testing"

	"github.com/czh0526/blockchain/core/vm"
)

func TestVM(t *testing.T) {
	var vmt VMTest
	if err := readJSONFile("./testdata/sha3_0.json", &vmt); err != nil {
		t.Fatal(err)
	}

	vmt.Run(vm.Config{Debug: false})
}
