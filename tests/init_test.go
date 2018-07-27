package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/czh0526/blockchain/params"
)

var (
	baseDir      = filepath.Join(".", "testdata")
	stateTestDir = filepath.Join(baseDir, "GeneralStateTests")
	vmTestDir    = filepath.Join(baseDir, "VMTests")
)

type testConfig struct {
	p      *regexp.Regexp
	config params.ChainConfig
}

type testFailure struct {
	p      *regexp.Regexp
	reason string
}

type testMatcher struct {
	configpat    []testConfig
	failpat      []testFailure
	skiploadpat  []*regexp.Regexp
	skipshortpat []*regexp.Regexp
}

func (tm *testMatcher) fails(pattern string, reason string) {
	if reason == "" {
		panic("empty fail reason")
	}
	tm.failpat = append(tm.failpat, testFailure{regexp.MustCompile(pattern), reason})
}

func (tm *testMatcher) skipShortMode(pattern string) {
	tm.skipshortpat = append(tm.skipshortpat, regexp.MustCompile(pattern))
}

func (tm *testMatcher) skipLoad(pattern string) {
	tm.skiploadpat = append(tm.skiploadpat, regexp.MustCompile(pattern))
}

func (tm *testMatcher) findSkip(name string) (reason string, skipload bool) {
	if testing.Short() {
		for _, re := range tm.skipshortpat {
			if re.MatchString(name) {
				return "skipped in -short mode", false
			}
		}
	}

	for _, re := range tm.skiploadpat {
		if re.MatchString(name) {
			return "skipped by skipload", true
		}
	}
	return "", false
}

func (tm *testMatcher) walk(t *testing.T, dir string, runTest interface{}) {
	// 检查目标文件夹
	dirinfo, err := os.Stat(dir)
	if os.IsNotExist(err) || !dirinfo.IsDir() {
		fmt.Fprintf(os.Stderr, "can't find test files in %s, did you clone the tests submodules? \n", dir)
		t.Skip("missing test files")
	}

	// 遍历目标文件夹
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		fmt.Printf("path = %s \n", path)
		name := filepath.ToSlash(strings.TrimPrefix(path, dir+string(filepath.Separator)))
		fmt.Printf("ToSlash() = %s \n", name)
		if info.IsDir() {
			if _, skipload := tm.findSkip(name + "/"); skipload {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".json" {
			t.Run(name, func(t *testing.T) {
				// 进行文件测试
				tm.runTestFile(t, path, name, runTest)
			})
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (tm *testMatcher) runTestFile(t *testing.T, path, name string, runTest interface{}) {
	if r, _ := tm.findSkip(name); r != "" {
		t.Skip(r)
	}
	t.Parallel()

	m := makeMapFromTestFunc(runTest)
	if err := readJSONFile(path, m.Addr().Interface()); err != nil {
		t.Fatal(err)
	}
}

func makeMapFromTestFunc(f interface{}) reflect.Value {
	stringT := reflect.TypeOf("")
	testingT := reflect.TypeOf((*testing.T)(nil))
	ftyp := reflect.TypeOf(f)
	if ftyp.Kind() != reflect.Func || ftyp.NumIn() != 3 || ftyp.NumOut() != 0 || ftyp.In(0) != testingT || ftyp.In(1) != stringT {
		panic(fmt.Sprintf("bad test function type: want func(*testing.T, string, <TestType>), have %s", ftyp))
	}
	testType := ftyp.In(2)
	mp := reflect.New(reflect.MapOf(stringT, testType))
	return mp.Elem()
}

func readJSONFile(fn string, value interface{}) error {
	file, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer file.Close()

	err = readJSON(file, value)
	if err != nil {
		return fmt.Errorf("%s in file %s", err.Error(), fn)
	}
	return nil
}

func readJSON(reader io.Reader, value interface{}) error {
	data, err := ioutil.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("error reading JSON file: %v", err)
	}
	if err = json.Unmarshal(data, &value); err != nil {
		if syntaxerr, ok := err.(*json.SyntaxError); ok {
			line := findLine(data, syntaxerr.Offset)
			return fmt.Errorf("JSON syntax error at line %v: %v", line, err)
		}
		return err
	}
	return nil
}

func findLine(data []byte, offset int64) (line int) {
	line = 1
	for i, r := range string(data) {
		if int64(i) >= offset {
			return
		}
		if r == '\n' {
			line++
		}
	}
	return
}
