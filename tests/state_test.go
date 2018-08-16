package tests

import (
	"testing"

	"github.com/czh0526/blockchain/core/vm"
)

func TestState(t *testing.T) {

	var stateTest StateTest
	if err := readJSONFile("./testdata/GeneralStateTests/stCreateTest", &stateTest); err != nil {
		t.Fatal(err)
	}

	subtests := stateTest.Subtests()
	for _, subtest := range subtests {
		stateTest.Run(subtest, vm.Config{Debug: false})
	}
}
