package tests

import (
	"testing"

	"github.com/czh0526/blockchain/core/vm"
)

func TestVM(t *testing.T) {
	var vmt VMTest
	if err := readJSONFile("./testdata/vmEnvironmentalInfo/calldatacopy_DataIndexTooHigh_return.json", &vmt); err != nil {
		t.Fatal(err)
	}

	if err := vmt.Run(vm.Config{Debug: false}); err != nil {
		t.Fatal(err)
	}
}
