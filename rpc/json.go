package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/czh0526/blockchain/log"
)

const (
	jsonrpcVersion           = "2.0"
	serviceMethodSeparator   = "_"
	subscribeMethodSuffix    = "_subscribe"
	unsubscribeMethodSuffix  = "_unsubscribe"
	notificationMethodSuffix = "_subscription"
)

type jsonRequest struct {
	Method  string          `json:"method"`
	Version string          `json:"jsonrpc"`
	Id      json.RawMessage `json:"id,omitempty"`
	Payload json.RawMessage `json:"params,omitempty"`
}

type jsonSuccessResponse struct {
	Version string      `json:"jsonrpc"`
	Id      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result"`
}

type jsonError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (err *jsonError) Error() string {
	if err.Message == "" {
		return fmt.Sprintf("json-rpc error %d", err.Code)
	}
	return err.Message
}

func (err *jsonError) ErrorCode() int {
	return err.Code
}

type jsonErrResponse struct {
	Version string      `json:"jsonrpc"`
	Id      interface{} `json:"id,omitempty"`
	Error   jsonError   `json:"error"`
}

type jsonSubscription struct {
	Subscription string      `json:"subscription"`
	Result       interface{} `json:"result,omitempty"`
}

type jsonNotification struct {
	Version string           `json:"jsonrpc"`
	Method  string           `json:"method"`
	Params  jsonSubscription `json:"params"`
}

type jsonCodec struct {
	closer sync.Once
	closed chan interface{}
	decMu  sync.Mutex
	decode func(v interface{}) error
	encMu  sync.Mutex
	encode func(v interface{}) error
	rw     io.ReadWriteCloser
}

func (c *jsonCodec) ReadRequestHeaders() ([]rpcRequest, bool, Error) {
	c.decMu.Lock()
	defer c.decMu.Unlock()

	var incomingMsg json.RawMessage
	if err := c.decode(&incomingMsg); err != nil {
		return nil, false, &invalidRequestError{err.Error()}
	}

	if isBatch(incomingMsg) {
		return parseBatchRequest(incomingMsg)
	}
	return parseRequest(incomingMsg)
}

func (c *jsonCodec) CreateResponse(id interface{}, reply interface{}) interface{} {
	if isHexNum(reflect.TypeOf(reply)) {
		return &jsonSuccessResponse{Version: jsonrpcVersion, Id: id, Result: fmt.Sprintf(`%#x`, reply)}
	}

	return &jsonSuccessResponse{Version: jsonrpcVersion, Id: id, Result: reply}
}

func (c *jsonCodec) CreateErrorResponse(id interface{}, err Error) interface{} {
	return &jsonErrResponse{Version: jsonrpcVersion, Id: id, Error: jsonError{Code: err.ErrorCode(), Message: err.Error()}}
}

func (c *jsonCodec) CreateErrorResponseWithInfo(id interface{}, err Error, info interface{}) interface{} {
	return &jsonErrResponse{Version: jsonrpcVersion, Id: id, Error: jsonError{Code: err.ErrorCode(), Message: err.Error(), Data: info}}
}

func (c *jsonCodec) Write(res interface{}) error {
	c.encMu.Lock()
	defer c.encMu.Unlock()

	return c.encode(res)
}

func (c *jsonCodec) CreateNotification(subid, namespace string, event interface{}) interface{} {
	if isHexNum(reflect.TypeOf(event)) {
		return &jsonNotification{
			Version: jsonrpcVersion,
			Method:  namespace + notificationMethodSuffix,
			Params: jsonSubscription{
				Subscription: subid,
				Result:       fmt.Sprintf(`%#x`, event),
			}}
	}

	return jsonNotification{
		Version: jsonrpcVersion,
		Method:  namespace + notificationMethodSuffix,
		Params: jsonSubscription{
			Subscription: subid,
			Result:       event,
		}}
}

func (c *jsonCodec) Close() {
	c.closer.Do(func() {
		close(c.closed)
		c.rw.Close()
	})
}

func (c *jsonCodec) Closed() <-chan interface{} {
	return c.closed
}

func NewCodec(rwc io.ReadWriteCloser, encode, decode func(v interface{}) error) ServerCodec {
	return &jsonCodec{
		closed: make(chan interface{}),
		encode: encode,
		decode: decode,
		rw:     rwc,
	}
}

func NewJSONCodec(rwc io.ReadWriteCloser) ServerCodec {
	enc := json.NewEncoder(rwc)
	dec := json.NewDecoder(rwc)
	dec.UseNumber()

	return &jsonCodec{
		closed: make(chan interface{}),
		encode: enc.Encode,
		decode: dec.Decode,
		rw:     rwc,
	}
}

func isBatch(msg json.RawMessage) bool {
	for _, c := range msg {
		if c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d {
			continue
		}
		return c == '['
	}
	return false
}

func parseRequest(incomingMsg json.RawMessage) ([]rpcRequest, bool, Error) {
	var in jsonRequest
	// Method, Version, Id, Payload
	if err := json.Unmarshal(incomingMsg, &in); err != nil {
		return nil, false, &invalidMessageError{err.Error()}
	}

	if err := checkReqId(in.Id); err != nil {
		return nil, false, &invalidMessageError{err.Error()}
	}

	// subscribe
	if strings.HasSuffix(in.Method, subscribeMethodSuffix) {
		// id, isPubSub
		reqs := []rpcRequest{{id: &in.Id, isPubSub: true}}
		if len(in.Payload) > 0 {
			var subscribeMethod [1]string
			// method
			if err := json.Unmarshal(in.Payload, &subscribeMethod); err != nil {
				log.Debug(fmt.Sprintf("Unable to parse subscription method: %v \n", err))
				return nil, false, &invalidRequestError{"Unable to parse subscription request"}
			}
			// service, method, params
			reqs[0].service, reqs[0].method = strings.TrimSuffix(in.Method, subscribeMethodSuffix), subscribeMethod[0]
			reqs[0].params = in.Payload
			return reqs, false, nil
		}
		return nil, false, &invalidRequestError{"Unable to parse subscription request"}
	}

	// unsubscribe
	if strings.HasSuffix(in.Method, unsubscribeMethodSuffix) {
		return []rpcRequest{{id: &in.Id, isPubSub: true, method: in.Method, params: in.Payload}}, false, nil
	}

	// RPC method call
	elems := strings.Split(in.Method, serviceMethodSeparator)
	if len(elems) != 2 {
		return nil, false, &methodNotFoundError{in.Method, ""}
	}

	if len(in.Payload) == 0 {
		return []rpcRequest{{service: elems[0], method: elems[1], id: &in.Id}}, false, nil
	}

	return []rpcRequest{{service: elems[0], method: elems[1], id: &in.Id, params: in.Payload}}, false, nil
}

func parseBatchRequest(incomingMsg json.RawMessage) ([]rpcRequest, bool, Error) {
	// RawMessage ==> jsonRequest
	var in []jsonRequest
	if err := json.Unmarshal(incomingMsg, &in); err != nil {
		return nil, false, &invalidMessageError{err.Error()}
	}

	// jsonRequest ==> rpcRequest
	requests := make([]rpcRequest, len(in))
	for i, r := range in {
		if err := checkReqId(r.Id); err != nil {
			return nil, false, &invalidMessageError{err.Error()}
		}
		id := &in[i].Id

		// subcribe
		if strings.HasSuffix(r.Method, subscribeMethodSuffix) {
			requests[i] = rpcRequest{id: id, isPubSub: true}
			if len(r.Payload) > 0 {
				var subscribeMethod [1]string
				if err := json.Unmarshal(r.Payload, &subscribeMethod); err != nil {
					log.Debug(fmt.Sprintf("Unable to parse subscription method: %v", err))
					return nil, false, &invalidRequestError{"Unable to parse subscription request."}
				}

				// example: eth_subscribe
				requests[i].service, requests[i].method = strings.TrimSuffix(r.Method, subscribeMethodSuffix), subscribeMethod[0]
				requests[i].params = r.Payload
				continue
			}
			return nil, true, &invalidRequestError{"Unable to parse (un)subscribe request arguments."}
		}

		// unsubscribe
		if strings.HasSuffix(r.Method, unsubscribeMethodSuffix) {
			requests[i] = rpcRequest{id: id, isPubSub: true, method: r.Method, params: r.Payload}
			continue
		}

		if len(r.Payload) == 0 {
			requests[i] = rpcRequest{id: id, params: nil}
		} else {
			requests[i] = rpcRequest{id: id, params: r.Payload}
		}
		if elem := strings.Split(r.Method, serviceMethodSeparator); len(elem) == 2 {
			requests[i].service, requests[i].method = elem[0], elem[1]
		} else {
			requests[i].err = &methodNotFoundError{r.Method, ""}
		}
	}

	return requests, true, nil
}

func (c *jsonCodec) ParseRequestArguments(argTypes []reflect.Type, params interface{}) ([]reflect.Value, Error) {
	if args, ok := params.(json.RawMessage); !ok {
		return nil, &invalidParamsError{"Invalid params supplied"}
	} else {
		return parsePositionalArguments(args, argTypes)
	}
}

func parsePositionalArguments(rawArgs json.RawMessage, types []reflect.Type) ([]reflect.Value, Error) {
	dec := json.NewDecoder(bytes.NewReader(rawArgs))
	if tok, _ := dec.Token(); tok != json.Delim('[') {
		return nil, &invalidParamsError{"non-array args"}
	}

	args := make([]reflect.Value, 0, len(types))
	for i := 0; dec.More(); i++ {
		if i >= len(types) {
			return nil, &invalidParamsError{fmt.Sprintf("too many arguments, want at most %d", len(types))}
		}
		argval := reflect.New(types[i])
		if err := dec.Decode(argval.Interface()); err != nil {
			return nil, &invalidParamsError{fmt.Sprintf("invalid argument %d: %v", i, err)}
		}
		if argval.IsNil() && types[i].Kind() != reflect.Ptr {
			return nil, &invalidParamsError{fmt.Sprintf("missing value for required argument %d", i)}
		}
		args = append(args, argval.Elem())
	}

	if _, err := dec.Token(); err != nil {
		return nil, &invalidParamsError{err.Error()}
	}
	// Set any missing args to nil.
	for i := len(args); i < len(types); i++ {
		if types[i].Kind() != reflect.Ptr {
			return nil, &invalidParamsError{fmt.Sprintf("missing value for required argument %d", i)}
		}
		args = append(args, reflect.Zero(types[i]))
	}
	return args, nil
}

func checkReqId(reqId json.RawMessage) error {
	if len(reqId) == 0 {
		return fmt.Errorf("missing request id")
	}
	if _, err := strconv.ParseFloat(string(reqId), 64); err == nil {
		return nil
	}
	var str string
	if err := json.Unmarshal(reqId, &str); err == nil {
		return nil
	}
	return fmt.Errorf("invalid request id")
}
