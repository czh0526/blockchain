package rpc

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/czh0526/blockchain/log"
)

func TestClientRequest(t *testing.T) {
	// 构建服务器
	server := newTestServer("service", new(Service))
	defer server.Stop()

	// 构建客户端
	client := DialInProc(server)
	defer client.Close()

	// 远程调用
	var resp Result
	if err := client.Call(&resp, "service_echo", "hello", 10, &Args{"world"}); err != nil {
		t.Fatal(err)
	}
	// 校验结果
	if !reflect.DeepEqual(resp, Result{"hello", 10, &Args{"world"}}) {
		t.Errorf("incorrect result %#v", resp)
	}
}

func TestClientCancelIPC(t *testing.T) { testClientCancel("ipc", t) }

func testClientCancel(transport string, t *testing.T) {
	// 构建服务器
	server := newTestServer("service", new(Service))
	defer server.Stop()

	// 构建通信通道的监听
	maxContextCancelTimeout := 300 * time.Millisecond
	fl := &flakeyListener{
		maxAcceptDelay: 1 * time.Second,
		maxKillTimeout: 600 * time.Millisecond,
	}

	// 构建客户端
	var client *Client
	switch transport {
	case "ws", "http":
		c, hs := httpTestClient(server, transport, fl)
		defer hs.Close()
		client = c
	case "ipc":
		c, l := ipcTestClient(server, fl)
		defer l.Close()
		client = c
	default:
		panic("unknown transport: " + transport)
	}

	t.Parallel()

	var (
		wg       sync.WaitGroup
		nreqs    = 10
		ncallers = 6
	)
	caller := func(index int) {
		defer wg.Done()

		// 发起 nreqs 个调用请求
		for i := 0; i < nreqs; i++ {
			var (
				ctx     context.Context
				cancel  func()
				timeout = time.Duration(rand.Int63n(int64(maxContextCancelTimeout)))
			)
			if index < ncallers/2 {
				// timeout 后，手动终止 Context
				ctx, cancel = context.WithCancel(context.Background())
				time.AfterFunc(timeout, cancel)
			} else {
				// timeout 后，自动终止 Context
				ctx, cancel = context.WithTimeout(context.Background(), timeout)
			}

			err := client.CallContext(ctx, nil, "service_sleep", 2*maxContextCancelTimeout)
			if err != nil {
				log.Debug(fmt.Sprint("got expected error:", err))
			} else {
				t.Errorf("no error for call with %c wait time", timeout)
			}
			cancel()
		}
	}

	// 并行运行多个 caller
	wg.Add(ncallers)
	for i := 0; i < ncallers; i++ {
		go caller(i)
	}
	wg.Wait()
}

func httpTestClient(srv *Server, transport string, fl *flakeyListener) (*Client, *httptest.Server) {
	var hs *httptest.Server
	switch transport {
	case "ws":
		hs = httptest.NewUnstartedServer(srv.WebsocketHandler([]string{"*"}))
	case "http":
		hs = httptest.NewUnstartedServer(srv)
	default:
		panic("unknown HTTP transport: " + transport)
	}

	if fl != nil {
		fl.Listener = hs.Listener
		hs.Listener = fl
	}

	hs.Start()
	client, err := Dial(transport + "://" + hs.Listener.Addr().String())
	if err != nil {
		panic(err)
	}
	return client, hs
}

// 启动 srv 监听，建立 Client ==> Srv 的连接
func ipcTestClient(srv *Server, fl *flakeyListener) (*Client, net.Listener) {
	endpoint := fmt.Sprintf("go-ethereum-test-ipc-%d-%d", os.Getpid(), rand.Int63())
	if runtime.GOOS == "windows" {
		endpoint = `\\.\pipe\` + endpoint
	} else {
		endpoint = os.TempDir() + "/" + endpoint
	}

	l, err := ipcListen(endpoint)
	if err != nil {
		panic(err)
	}

	if fl != nil {
		fl.Listener = l
		l = fl
	}

	go srv.ServeListener(l)
	client, err := Dial(endpoint)
	if err != nil {
		panic(err)
	}

	return client, l
}

func TestClientHTTP(t *testing.T) {
	server := newTestServer("service", new(Service))
	defer server.Stop()

	client, hs := httpTestClient(server, "http", nil)
	defer hs.Close()
	defer client.Close()

	var (
		results    = make([]Result, 100)
		errc       = make(chan error)
		wantResult = Result{"a", 1, new(Args)}
	)

	// 发送请求
	for i := range results {
		i := i
		go func() {
			errc <- client.Call(
				&results[i],
				"service_echo",
				wantResult.String,
				wantResult.Int,
				wantResult.Args)
		}()
	}

	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for i := range results {
		select {
		case err := <-errc:
			if err != nil {
				t.Fatal(err)
			}
		case <-timeout.C:
			t.Fatalf("timeout (got %d/%d results)", i+1, len(results))
		}
	}

	for i := range results {
		if !reflect.DeepEqual(results[i], wantResult) {
			t.Errorf("result %d mismatch: got %#v, want %#v", i, results[i], wantResult)
		}
	}
}

func TestShadowHTTP(t *testing.T) {
	client, err := Dial("http://127.0.0.1:8545")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	if err := client.Call(nil, "shadow_setName", "Cai,Zhihong"); err != nil {
		panic(err)
	}
}

func TestShadowWebsocket(t *testing.T) {
	client, err := Dial("ws://127.0.0.1:8546")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	var name string
	if err := client.Call(&name, "shadow_getName"); err != nil {
		panic(err)
	}

	fmt.Printf("shadow.GetName() = %v \n", name)
}

func newTestServer(serviceName string, service interface{}) *Server {
	server := NewServer()
	if err := server.RegisterName(serviceName, service); err != nil {
		panic(err)
	}
	return server
}

type flakeyListener struct {
	net.Listener
	maxKillTimeout time.Duration
	maxAcceptDelay time.Duration
}

func (l *flakeyListener) Accept() (net.Conn, error) {
	// 随机等待一段时间后，处理连接请求
	delay := time.Duration(rand.Int63n(int64(l.maxAcceptDelay)))
	time.Sleep(delay)

	// 处理连接请求
	c, err := l.Listener.Accept()
	if err == nil {
		// 随机等待一段时间后，关闭连接
		timeout := time.Duration(rand.Int63n(int64(l.maxKillTimeout)))
		time.AfterFunc(timeout, func() {
			log.Debug(fmt.Sprintf("killing conn %v after %v", c.LocalAddr(), timeout))
			c.Close()
		})
	}
	return c, err
}
