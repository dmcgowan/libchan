package libchan

import (
	"errors"
	"io"
)

var (
	ErrIncompatibleSender   = errors.New("incompatible sender")
	ErrIncompatibleReceiver = errors.New("incompatible receiver")
)

type ReceiverFrom interface {
	ReceiveFrom(Receiver) (int, error)
}

type SenderTo interface {
	SendTo(Sender) (int, error)
}

// Copy copies from a receiver to a sender until an EOF is
// received.  The number of copies made is returned along
// with any error that may have halted copying prior to an EOF.
func Copy(w Sender, r Receiver) (int, error) {
	if senderTo, ok := r.(SenderTo); ok {
		if n, err := senderTo.SendTo(w); err != ErrIncompatibleSender {
			return n, err
		}
	}
	if receiverFrom, ok := w.(ReceiverFrom); ok {
		if n, err := receiverFrom.ReceiveFrom(r); err != ErrIncompatibleReceiver {
			return n, err
		}
	}

	var n int
	for {
		var m interface{}
		err := r.Receive(&m)
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return n, err
			}
		}

		err = w.Send(m)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
