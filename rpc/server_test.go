package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
)

type Service struct{}

type Args struct {
	S string
}

type Result struct {
	String string
	Int    int
	Args   *Args
}

func (s *Service) NoArgsRets() {
}

func (s *Service) Echo(str string, i int, args *Args) Result {
	return Result{str, i, args}
}

func (s *Service) EchoWithCtx(ctx context.Context, str string, i int, args *Args) Result {
	return Result{str, i, args}
}

func (s *Service) Sleep(ctx context.Context, duration time.Duration) {
	select {
	case <-time.After(duration):
	case <-ctx.Done():
	}
}

func (s *Service) Rets() (string, error) {
	return "", nil
}

func (s *Service) InvalidRets1() (error, string) {
	return nil, ""
}

func (s *Service) InvalidRets2() (string, string) {
	return "", ""
}

func (s *Service) InvalidRets3() (string, string, error) {
	return "", "", nil
}

func (s *Service) Subscription(ctx context.Context) (*Subscription, error) {
	return nil, nil
}

func TestReflect(t *testing.T) {
	srv := Service{}
	type1 := reflect.ValueOf(srv).Type()
	tname1 := type1.Name()
	refTName1 := reflect.Indirect(reflect.ValueOf(srv)).Type().Name()
	fmt.Printf("srv type name = %q, srv ref type name = %q\n", tname1, refTName1)

	psrv := new(Service)
	type2 := reflect.ValueOf(psrv).Type()
	tnames := type2.Name()
	refTName2 := reflect.Indirect(reflect.ValueOf(psrv)).Type().Name()
	fmt.Printf("srv type name = %q, srv ref type name = %q\n", tnames, refTName2)
}

func TestServerRegistryName(t *testing.T) {
	server := NewServer()
	service := new(Service)

	if err := server.RegisterName("calc", service); err != nil {
		t.Fatal("%v", err)
	}

	if len(server.services) != 2 {
		t.Fatalf("Expected 2 service entries, got %d", len(server.services))
	}

	svc, ok := server.services["calc"]
	if !ok {
		t.Fatalf("Expected service calc to be registered")
	}

	if len(svc.callbacks) != 5 {
		t.Errorf("Expected 5 callbacks for service 'calc', got %d", len(svc.callbacks))
	}

	if len(svc.subscriptions) != 1 {
		t.Errorf("Expected 1 subscription for service 'calc', got %d", len(svc.subscriptions))
	}
}

func TestServerMethodExecution(t *testing.T) {
	testServerMethodExecution(t, "echo")
}

func testServerMethodExecution(t *testing.T, method string) {
	server := NewServer()
	service := new(Service)

	if err := server.RegisterName("test", service); err != nil {
		t.Fatalf("%v", err)
	}

	stringArg := "string arg"
	intArg := 1122
	argsArg := &Args{"abcde"}
	params := []interface{}{stringArg, intArg, argsArg}

	request := map[string]interface{}{
		"id":      12345,
		"method":  "test_" + method,
		"jsonrpc": "2.0",
		"params":  params,
	}

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()

	go server.ServeCodec(NewJSONCodec(serverConn), OptionMethodInvocation)

	out := json.NewEncoder(clientConn)
	in := json.NewDecoder(clientConn)

	if err := out.Encode(request); err != nil {
		t.Fatal(err)
	}

	response := jsonSuccessResponse{Result: &Result{}}
	if err := in.Decode(&response); err != nil {
		fmt.Println(err)
	}

	//<-time.After(time.Minute * 5)
}
