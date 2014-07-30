package libchan

import (
	"errors"
	"sync"
)

// SendChannelBinder represents a type that can have a Sender
// bound to it.
type SendChannelBinder interface {
	// Bind binds a nested send channel to a transport channel.  This
	// should be called by the transport upon sending a nested sender.
	Bind(Sender, Receiver) error
}

// ReceiveChannelBinder represents a type that can have a Receiver
// bound to it.
type ReceiveChannelBinder interface {
	// Bind binds a nested receive channel to a transport channel.  This
	// should be called by the transport upon sending a nested receiver.
	Bind(Receiver, Sender) error
}

type nestedChannel struct {
	sender   Sender
	receiver Receiver
	bindLock sync.Mutex
	bound    chan struct{}
}

func (n *nestedChannel) Bind(s Sender, r Receiver, isSend bool) error {
	n.bindLock.Lock()
	defer n.bindLock.Unlock()
	if n.sender != nil || n.receiver != nil {
		return errors.New("already bound")
	}
	n.sender = s
	n.receiver = r
	close(n.bound)
	return nil
}

// nestedSender represents nested sender channel that is bound
// to a transport when either end of the channel is sent.
type nestedSender struct {
	channel *nestedChannel
}

func (n *nestedSender) Send(message interface{}) error {
	<-n.channel.bound
	return n.channel.sender.Send(message)
}

func (n *nestedSender) Close() error {
	return n.channel.sender.Close()
}

func (n *nestedSender) Bind(s Sender, r Receiver) error {
	return n.channel.Bind(s, r, false)
}

// nestedReceiver represents a nested receiver channel that is
// bound to a transport when either end of the channel is sent.
type nestedReceiver struct {
	channel *nestedChannel
}

func (n *nestedReceiver) Receive(message interface{}) error {
	<-n.channel.bound
	return n.channel.receiver.Receive(message)
}

func (n *nestedReceiver) Bind(r Receiver, s Sender) error {
	return n.channel.Bind(s, r, true)
}

// NewNestedChannel returns a nested Sender Receiver pair to be
// sent on a transport.  The channel will block until either the
// sender or the receiver has been bound by a transport.  Channel
// direction is determined on bind.
func NewNestedChannel() (Receiver, Sender) {
	channel := &nestedChannel{
		bound: make(chan struct{}),
	}
	s := &nestedSender{channel: channel}
	r := &nestedReceiver{channel: channel}
	return r, s
}
