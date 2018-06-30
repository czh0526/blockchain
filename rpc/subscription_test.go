package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
)

func TestNotifications(t *testing.T) {
	server := NewServer()
	service := &NotificationTestService{}

	if err := server.RegisterName("eth", service); err != nil {
		t.Fatalf("unable to register test service: %v", err)
	}

	clientConn, serverConn := net.Pipe()
	go server.ServeCodec(NewJSONCodec(serverConn), OptionMethodInvocation|OptionSubscriptions)

	out := json.NewEncoder(clientConn)
	in := json.NewDecoder(clientConn)

	n := 5
	val := 12345
	request := map[string]interface{}{
		"id":      1,
		"method":  "eth_subscribe",
		"jsonrpc": "2.0",
		"params":  []interface{}{"someSubscription", n, val},
	}

	if err := out.Encode(request); err != nil {
		t.Fatal(err)
	}

	var subid string
	response := jsonSuccessResponse{Result: subid}
	if err := in.Decode(&response); err != nil {
		t.Fatal(err)
	}

	var ok bool
	if subid, ok = response.Result.(string); !ok {
		t.Fatalf("expected subscription id, got %T", response.Result)
	}
	fmt.Printf("subscription id = %v \n", subid)

	for i := 0; i < n; i++ {
		var notification jsonNotification
		if err := in.Decode(&notification); err != nil {
			t.Fatalf("%v", err)
		}

		if int(notification.Params.Result.(float64)) != val+i {
			t.Fatalf("expected %d, got %d", val+i, notification.Params.Result)
		}
	}
}

type NotificationTestService struct {
	mu           sync.Mutex
	unsubscribed bool

	gotHangSubscriptionReq  chan struct{}
	unblockHangSubscription chan struct{}
}

func (s *NotificationTestService) Echo(i int) int {
	return i
}

func (s *NotificationTestService) SomeSubscription(ctx context.Context, n, val int) (*Subscription, error) {
	notifier, supported := NotifierFromContext(ctx)
	if !supported {
		return nil, ErrNotificationsUnsupported
	}

	subscription := notifier.CreateSubscription()

	go func() {
		time.Sleep(5 * time.Second)
		for i := 0; i < n; i++ {
			if err := notifier.Notify(subscription.ID, val+i); err != nil {
				return
			}
		}

		select {
		case <-notifier.Closed():
			s.mu.Lock()
			s.unsubscribed = true
			s.mu.Unlock()
		case <-subscription.Err():
			s.mu.Lock()
			s.unsubscribed = true
			s.mu.Unlock()
		}
	}()

	return subscription, nil
}

func (s *NotificationTestService) wasUnsubCallbackCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unsubscribed
}

func (s *NotificationTestService) Unsubscribe(subid string) {
	s.mu.Lock()
	s.unsubscribed = true
	s.mu.Unlock()
}
