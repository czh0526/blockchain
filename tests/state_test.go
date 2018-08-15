package tests

import "testing"

func TestState(t *testing.T) {

	var stateTest StateTest
	if err := readJSONFile("", &stateTest); err != nil {
		t.Fatal(err)
	}

	stateTest.Run()
}
