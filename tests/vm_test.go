package tests

import "testing"

func TestVM(t *testing.T) {
	t.Parallel()

	vmt := new(testMatcher)
	vmt.fails("^vmSystemOperationsTest.json/createNameRegistrator$", "fails without parallel execution")

	vmt.skipLoad(`^vmInputLimits(Light)?.json`)

	vmt.skipShortMode("^vmPerformanceTest.json")
	vmt.skipShortMode("^vmInputLimits(Light)?.json")

	vmt.walk(t, vmTestDir, func(t *testing.T, name string, test *VMTest) {
		
	})
}
