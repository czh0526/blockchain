package rpc

import (
	"bytes"
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/czh0526/blockchain/log"
)

var (
	ErrClientQuit                = errors.New("client is closed")
	ErrNoResult                  = errors.New("no result in JSON-RPC response")
	ErrSubscriptionQueueOverflow = errors.New("subscription queue overflow")
)

const (
	tcpKeepAliveInterval = 30 * time.Second
	defaultDialTimeout   = 10 * time.Second
	defaultWriteTimeout  = 10 * time.Second
	subscribeTimeout     = 5 * time.Second
)

const (
	maxClientSubscriptionBuffer = 8000
)

var nullAddr, _ = net.ResolveTCPAddr("tcp", "127.0.0.1:0")

type httpConn struct {
	client    *http.Client
	req       *http.Request
	closeOnce sync.Once
	closed    chan struct{}
}

func (hc *httpConn) LocalAddr() net.Addr              { return nullAddr }
func (hc *httpConn) RemoteAddr() net.Addr             { return nullAddr }
func (hc *httpConn) SetReadDeadline(time.Time) error  { return nil }
func (hc *httpConn) SetWriteDeadline(time.Time) error { return nil }
func (hc *httpConn) SetDeadline(time.Time) error      { return nil }
func (hc *httpConn) Write([]byte) (int, error)        { panic("Write called") }
func (hc *httpConn) Read(b []byte) (int, error) {
	<-hc.closed
	return 0, io.EOF
}

func (hc *httpConn) Close() error {
	hc.closeOnce.Do(func() { close(hc.closed) })
	return nil
}

func (hc *httpConn) doRequest(ctx context.Context, msg interface{}) (io.ReadCloser, error) {
	// 构建 Body 对象
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	// 构建 http.Request 对象
	req := hc.req.WithContext(ctx)
	req.Body = ioutil.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))

	// 完成一次 http 的请求响应
	resp, err := hc.client.Do(req)
	if err != nil {
		return nil, err
	}

	// 错误处理
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.Body, errors.New(resp.Status)
	}

	return resp.Body, nil
}

type jsonrpcMessage struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id, omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Error   *jsonError      `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,imptempty"`
}

func (msg *jsonrpcMessage) isNotification() bool {
	return msg.ID == nil && msg.Method != ""
}

func (msg *jsonrpcMessage) isResponse() bool {
	return msg.hasValidID() && msg.Method == "" && len(msg.Params) == 0
}

func (msg *jsonrpcMessage) hasValidID() bool {
	return len(msg.ID) > 0 && msg.ID[0] != '{' && msg.ID[0] != '['
}

func (msg *jsonrpcMessage) String() string {
	b, _ := json.Marshal(msg)
	return string(b)
}

type requestOp struct {
	ids  []json.RawMessage
	err  error
	resp chan *jsonrpcMessage
	sub  *ClientSubscription
}

func (op *requestOp) wait(ctx context.Context) (*jsonrpcMessage, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-op.resp:
		return resp, op.err
	}
}

type ClientSubscription struct {
	client    *Client
	etype     reflect.Type
	channel   reflect.Value
	namespace string
	subid     string
	in        chan json.RawMessage

	quitOnce sync.Once
	quit     chan struct{}
	errOnce  sync.Once
	err      chan error
}

func newClientSubscription(c *Client, namespace string, channel reflect.Value) *ClientSubscription {
	sub := &ClientSubscription{
		client:    c,
		namespace: namespace,
		etype:     channel.Type().Elem(),
		channel:   channel,
		quit:      make(chan struct{}),
		err:       make(chan error, 1),
		in:        make(chan json.RawMessage),
	}
	return sub
}

func (sub *ClientSubscription) quitWithError(err error, unsubscribeServer bool) {
	sub.quitOnce.Do(func() {
		close(sub.quit)
		if unsubscribeServer {
			sub.requestUnsubscribe()
		}
		if err != nil {
			if err == ErrClientQuit {
				err = nil
			}
			sub.err <- err
		}
	})
}

func (sub *ClientSubscription) requestUnsubscribe() error {
	var result interface{}
	return sub.client.Call(&result, sub.namespace+unsubscribeMethodSuffix, sub.subid)
}

func (sub *ClientSubscription) deliver(result json.RawMessage) (ok bool) {
	select {
	case sub.in <- result:
		return true
	case <-sub.quit:
		return false
	}
}

func (sub *ClientSubscription) start() {
	sub.quitWithError(sub.forward())
}

func (sub *ClientSubscription) forward() (err error, unsubscribeServer bool) {
	cases := []reflect.SelectCase{
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(sub.quit)},
		{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(sub.in)},
		{Dir: reflect.SelectSend, Chan: sub.channel},
	}

	buffer := list.New()
	defer buffer.Init()
	for {
		var chosen int
		var recv reflect.Value
		if buffer.Len() == 0 {

		} else {

		}

		switch chosen {
		case 0: // <-sub.quit
			return nil, false
		case 1: // <-sub.in
			val, err := sub.unmarshal(recv.Interface().(json.RawMessage))
			if err != nil {
				return err, true
			}
			if buffer.Len() == maxClientSubscriptionBuffer {
				return ErrSubscriptionQueueOverflow, true
			}
			buffer.PushBack(val)
		case 2:
			cases[2].Send = reflect.Value{}
			buffer.Remove(buffer.Front())
		}
	}
}

func (sub *ClientSubscription) unmarshal(result json.RawMessage) (interface{}, error) {
	// 利用 reflect.Type 构建对象
	val := reflect.New(sub.etype)
	// 反序列化信息，填充对象
	err := json.Unmarshal(result, val.Interface())
	return val.Elem().Interface(), err
}

type Client struct {
	idCounter   uint32
	connectFunc func(ctx context.Context) (net.Conn, error)
	isHTTP      bool

	writeConn net.Conn

	// for dispatch
	close       chan struct{}
	didQuit     chan struct{}
	reconnected chan net.Conn
	readErr     chan error
	readResp    chan []*jsonrpcMessage
	requestOp   chan *requestOp
	sendDone    chan error
	respWait    map[string]*requestOp
	subs        map[string]*ClientSubscription
}

func newClient(initctx context.Context, connectFunc func(context.Context) (net.Conn, error)) (*Client, error) {
	conn, err := connectFunc(initctx)
	if err != nil {
		return nil, err
	}
	_, isHTTP := conn.(*httpConn)
	c := &Client{
		writeConn:   conn,
		isHTTP:      isHTTP,
		connectFunc: connectFunc,
		close:       make(chan struct{}),
		didQuit:     make(chan struct{}),
		reconnected: make(chan net.Conn),
		readErr:     make(chan error),
		readResp:    make(chan []*jsonrpcMessage),
		requestOp:   make(chan *requestOp),
		sendDone:    make(chan error, 1),
		respWait:    make(map[string]*requestOp),
		subs:        make(map[string]*ClientSubscription),
	}
	if !isHTTP {
		go c.dispatch(conn)
	}
	return c, nil
}

// 主循环
func (c *Client) dispatch(conn net.Conn) {
	// 启动例程，向 c.readResp 中投递消息
	go c.read(conn)

	var (
		lastOp        *requestOp
		requestOpLock = c.requestOp
		reading       = true
	)
	defer close(c.didQuit)
	defer func() {
		c.closeRequestOps(ErrClientQuit)
		conn.Close()
		if reading {
			for {
				select {
				case <-c.readResp:
				case <-c.readErr:
					return
				}
			}
		}
	}()

	for {
		select {
		case <-c.close:
			return

		// 处理消息接收请求
		case batch := <-c.readResp:
			for _, msg := range batch {
				switch {
				case msg.isNotification():
					log.Debug(fmt.Sprintf("[RPC]: Client read notification <== %v", msg))
					c.handleNotification(msg)
				case msg.isResponse():
					log.Debug(fmt.Sprintf("[RPC]: Client read response <== %v", msg))
					c.handleResponse(msg)
				default:
					log.Debug(fmt.Sprintf("[RPC]: Client drop wrird message <== %v", msg))
				}
			}
		// 处理接收错误
		case err := <-c.readErr:
			log.Debug(fmt.Sprintf("[RPC]: Client read message error: %v", err))
			c.closeRequestOps(err)
			conn.Close()
			reading = false

		case newconn := <-c.reconnected:
			log.Debug("<-reconnected", "reading", reading, "remote", conn.RemoteAddr())
			if reading {
				conn.Close()
				<-c.readErr
			}
			go c.read(newconn)
			reading = true
			conn = newconn
		// 处理消息发送请求
		case op := <-requestOpLock:
			requestOpLock = nil
			lastOp = op
			for _, id := range op.ids {
				c.respWait[string(id)] = op
			}
		case err := <-c.sendDone:
			if err != nil {
				for _, id := range lastOp.ids {
					delete(c.respWait, string(id))
				}
			}

			requestOpLock = c.requestOp
			lastOp = nil
		}
	}
}

func (c *Client) Call(result interface{}, method string, args ...interface{}) error {
	ctx := context.Background()
	return c.CallContext(ctx, result, method, args...)
}

func (c *Client) CallContext(ctx context.Context, result interface{}, method string, args ...interface{}) error {
	// 构建消息
	msg, err := c.newMessage(method, args...)
	if err != nil {
		return err
	}
	fmt.Printf("msg = %s \n", msg)

	// 构建 requestOp, 用于接收消息
	op := &requestOp{ids: []json.RawMessage{msg.ID}, resp: make(chan *jsonrpcMessage, 1)}

	// 发送消息
	if c.isHTTP {
		err = c.sendHTTP(ctx, op, msg)
	} else {
		err = c.send(ctx, op, msg)
	}
	if err != nil {
		return err
	}

	switch resp, err := op.wait(ctx); {
	case err != nil:
		return err
	case resp.Error != nil:
		return resp.Error
	case len(resp.Result) == 0:
		return ErrNoResult
	default:
		return json.Unmarshal(resp.Result, &result)
	}
}

func (c *Client) newMessage(method string, paramsIn ...interface{}) (*jsonrpcMessage, error) {
	params, err := json.Marshal(paramsIn)
	if err != nil {
		return nil, err
	}

	return &jsonrpcMessage{Version: "2.0", ID: c.nextID(), Method: method, Params: params}, nil
}

func (c *Client) nextID() json.RawMessage {
	id := atomic.AddUint32(&c.idCounter, 1)
	return []byte(strconv.FormatUint(uint64(id), 10))
}

// 作为单独例程启动，持续向 c.readResp 中投递响应消息
func (c *Client) read(conn net.Conn) error {
	var (
		buf json.RawMessage
		dec = json.NewDecoder(conn)
	)
	readMessage := func() (rs []*jsonrpcMessage, err error) {
		buf = buf[:0]
		if err = dec.Decode(&buf); err != nil {
			return nil, err
		}
		if isBatch(buf) {
			err = json.Unmarshal(buf, &rs)
		} else {
			rs = make([]*jsonrpcMessage, 1)
			err = json.Unmarshal(buf, &rs[0])
		}
		return rs, err
	}

	for {
		resp, err := readMessage()
		if err != nil {
			c.readErr <- err
			return err
		}
		c.readResp <- resp
	}
}

func (c *Client) closeRequestOps(err error) {
	didClose := make(map[*requestOp]bool)

	for id, op := range c.respWait {
		delete(c.respWait, id)

		if !didClose[op] {
			op.err = err
			close(op.resp)
			didClose[op] = true
		}
	}

	for id, sub := range c.subs {
		delete(c.subs, id)
		sub.quitWithError(err, false)
	}
}

func (c *Client) send(ctx context.Context, op *requestOp, msg interface{}) error {
	select {
	case c.requestOp <- op:
		log.Debug(fmt.Sprintf("[RPC]: client send message ==> %v", msg))
		err := c.write(ctx, msg)
		c.sendDone <- err
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-c.didQuit:
		return ErrClientQuit
	}
}

func (c *Client) sendHTTP(ctx context.Context, op *requestOp, msg interface{}) error {
	hc := c.writeConn.(*httpConn)
	log.Debug(fmt.Sprintf("[RPC]: client send http message ==> %v", msg))
	respBody, err := hc.doRequest(ctx, msg)
	if respBody != nil {
		defer respBody.Close()
	}

	if err != nil {
		if respBody != nil {
			buf := new(bytes.Buffer)
			if _, err2 := buf.ReadFrom(respBody); err2 == nil {
				return fmt.Errorf("%v %v", err, buf.String())
			}
		}
		return err
	}

	var respmsg jsonrpcMessage
	if err := json.NewDecoder(respBody).Decode(&respmsg); err != nil {
		return err
	}
	op.resp <- &respmsg
	return nil
}

func (c *Client) write(ctx context.Context, msg interface{}) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(defaultWriteTimeout)
	}

	if c.writeConn == nil {
		if err := c.reconnect(ctx); err != nil {
			return err
		}
	}
	c.writeConn.SetWriteDeadline(deadline)
	err := json.NewEncoder(c.writeConn).Encode(msg)
	if err != nil {
		c.writeConn = nil
	}
	return err
}

func (c *Client) reconnect(ctx context.Context) error {
	newconn, err := c.connectFunc(ctx)
	if err != nil {
		log.Debug(fmt.Sprintf("reconnect failed: %v", err))
		return err
	}
	select {
	case c.reconnected <- newconn:
		c.writeConn = newconn
		return nil
	case <-c.didQuit:
		newconn.Close()
		return ErrClientQuit
	}
}

func (c *Client) handleResponse(msg *jsonrpcMessage) {
	op := c.respWait[string(msg.ID)]
	if op == nil {
		log.Debug("unsolicited response", "msg", msg)
		return
	}
	delete(c.respWait, string(msg.ID))

	if op.sub == nil {
		op.resp <- msg
		return
	}

	defer close(op.resp)
	if msg.Error != nil {
		op.err = msg.Error
		return
	}

	if op.err = json.Unmarshal(msg.Result, &op.sub.subid); op.err == nil {
		go op.sub.start()
		c.subs[op.sub.subid] = op.sub
	}
}

func (c *Client) handleNotification(msg *jsonrpcMessage) {
	if !strings.HasSuffix(msg.Method, notificationMethodSuffix) {
		log.Debug("dropping non-subscription message", "msg", msg)
		return
	}
	var subResult struct {
		ID     string          `json:"subscription"`
		Result json.RawMessage `json:"result"`
	}

	if err := json.Unmarshal(msg.Params, &subResult); err != nil {
		log.Debug("dropping invalid subscription message", "msg", msg)
		return
	}

	if c.subs[subResult.ID] != nil {
		c.subs[subResult.ID].deliver(subResult.Result)
	}
}

func (c *Client) Close() {
	if c.isHTTP {
		return
	}

	select {
	case c.close <- struct{}{}:
		<-c.didQuit
	case <-c.didQuit:
	}
}

func Dial(rawurl string) (*Client, error) {
	return DialContext(context.Background(), rawurl)
}

func DialContext(ctx context.Context, rawurl string) (*Client, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "http", "https":
		return DialHTTP(rawurl)
	case "ws", "wss":
		return DialWebsocket(ctx, rawurl, "")
	case "":
		return DialIPC(ctx, rawurl)
	default:
		return nil, fmt.Errorf("no known transport for URL scheme %q", u.Scheme)
	}
}
