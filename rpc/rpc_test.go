package rpc

import (
	"log"
	"reflect"
	"testing"

	"github.com/docker/libchan"
)

type Simple interface {
	Add(int)
	Value() int
}

type serverHandler struct {
	v int
}

func (s *serverHandler) Add(i int) {
	s.v = s.v + i
}

func (s *serverHandler) Value() int {
	return s.v
}

type clientHandler struct {
	sender libchan.Sender
}

func (s *clientHandler) Add(i int) {
	if err := CallAsync(s.sender, "Add", i); err != nil {
		log.Printf("Error calling add: %s", err)
	}
}

func (s *clientHandler) Value() int {
	ret, err := Call(s.sender, "Value")
	if err != nil {
		log.Printf("Error getting value: %s", err)
		return 0
	}
	if len(ret) != 1 {
		log.Printf("Bad receive value: %#v", ret)
	}
	i, ok := ret[0].(int64)
	if !ok {
		log.Printf("Unexpected receive type: %#v", ret[0])
		return 0
	}

	return int(i)
}

func NewSimpleClient(s libchan.Sender) Simple {
	return &clientHandler{
		sender: s,
	}
}

func TestSimpleInterface(t *testing.T) {
	closeServer := make(chan struct{})
	recv, send := libchan.Pipe()
	go func() {
		s := NewServer(recv, reflect.ValueOf(new(Simple)).Elem().Type(), reflect.ValueOf(new(serverHandler)))
		if err := s.Serve(); err != nil {
			t.Errorf("Server error: %s", err)
		}
		close(closeServer)
	}()

	simple := NewSimpleClient(send)

	simple.Add(1)
	if i := simple.Value(); i != 1 {
		t.Fatalf("Wrong value (1): %d", i)
	}
	simple.Add(1)
	if i := simple.Value(); i != 2 {
		t.Fatalf("Wrong value (2): %d", i)
	}
	simple.Add(10)
	if i := simple.Value(); i != 12 {
		t.Fatalf("Wrong value (12): %d", i)
	}
	simple.Add(88)
	if i := simple.Value(); i != 100 {
		t.Fatalf("Wrong value (100): %d", i)
	}

	if err := send.Close(); err != nil {
		t.Fatalf("Error closing sender: %s", err)
	}

	<-closeServer
}
