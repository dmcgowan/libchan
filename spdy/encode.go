package spdy

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"time"

	"github.com/dmcgowan/go/codec"
	"github.com/docker/libchan"
)

func (s *Transport) encodeChannel(v reflect.Value) ([]byte, error) {
	rc := v.Interface().(channel)
	if rc.stream == nil {
		return nil, errors.New("bad type")
	}
	if rc.session != s {
		return nil, errors.New("cannot decode channel from different session")
	}

	// Get stream identifier?
	referenceID := rc.referenceID
	buf := make([]byte, 9)
	if rc.direction == inbound {
		buf[0] = 0x02 // Reverse direction
	} else if rc.direction == outbound {
		buf[0] = 0x01 // Reverse direction
	} else {
		return nil, errors.New("invalid direction")
	}
	binary.BigEndian.PutUint64(buf[1:], referenceID)
	return buf, nil
}

func (s *Transport) decodeChannel(v reflect.Value, b []byte) error {
	rc := v.Interface().(channel)

	if b[0] == 0x01 {
		rc.direction = inbound
	} else if b[0] == 0x02 {
		rc.direction = outbound
	} else {
		return errors.New("unexpected direction")
	}

	referenceID := binary.BigEndian.Uint64(b[1:])
	c := s.getChannel(referenceID)
	if c == nil {
		return errors.New("channel does not exist")
	}
	v.Set(reflect.ValueOf(*c))

	return nil
}

func (s *Transport) encodeStream(v reflect.Value) ([]byte, error) {
	bs := v.Interface().(byteStream)
	if bs.ReferenceID == 0 {
		return nil, errors.New("bad type")
	}
	var buf [8]byte
	written := binary.PutUvarint(buf[:], uint64(bs.ReferenceID))

	return buf[:written], nil
}

func (s *Transport) decodeStream(v reflect.Value, b []byte) error {
	referenceID, readN := binary.Uvarint(b)
	if readN == 0 {
		return errors.New("bad reference id")
	}

	bs := s.getByteStream(referenceID)
	if bs != nil {
		v.Set(reflect.ValueOf(*bs))
	}

	return nil
}

func (s *Transport) encodeWrapper(v reflect.Value) ([]byte, error) {
	wrapper := v.Interface().(libchan.ByteStreamWrapper)
	bs, err := s.createByteStream()
	if err != nil {
		return nil, err
	}

	go func() {
		io.Copy(bs, wrapper)
		bs.Close()
	}()

	go func() {
		io.Copy(wrapper, bs)
		wrapper.Close()
	}()

	return s.encodeStream(reflect.ValueOf(bs).Elem())
}

func (s *Transport) decodeWrapper(v reflect.Value, b []byte) error {
	bs := &byteStream{}
	s.decodeStream(reflect.ValueOf(bs).Elem(), b)
	v.FieldByName("ReadWriteCloser").Set(reflect.ValueOf(bs))
	return nil
}

func (s *Transport) waitConn(network, local, remote string, timeout time.Duration) (net.Conn, error) {
	timeoutChan := time.After(timeout)
	connChan := make(chan net.Conn)

	go func() {
		defer close(connChan)
		s.netConnC.L.Lock()
		defer s.netConnC.L.Unlock()
		key := addrKey(local, remote)
		networkType, ok := s.networks[network]
		for !ok {
			select {
			case <-timeoutChan:
				return // Don't wait, timeout already occured
			default:
			}
			s.netConnC.Wait()
			networkType, ok = s.networks[network]
		}
		networks := s.netConns[networkType]
		conn, ok := networks[key]
		for !ok {
			select {
			case <-timeoutChan:
				return // Don't bother, timeout already occured
			default:
			}
			s.netConnC.Wait()
			conn, ok = networks[key]
		}
		connChan <- conn
	}()

	select {
	case conn := <-connChan:
		return conn, nil
	case <-timeoutChan:
		return nil, errors.New("timeout")
	}
}

func encodeString3(s1, s2, s3 string) ([]byte, error) {
	if len(s1) > 0x7F || len(s2) > 0x7F || len(s3) > 0x7F {
		return nil, errors.New("invalid string length")
	}
	b := make([]byte, len(s1)+len(s2)+len(s3)+3)
	b[0] = byte(len(s1))
	copy(b[1:], s1)
	b[len(s1)+1] = byte(len(s2))
	copy(b[len(s1)+2:], s2)
	b[len(s1)+len(s2)+2] = byte(len(s3))
	copy(b[len(s1)+len(s2)+3:], s3)
	return b, nil
}

func readString(b []byte) (int, string, error) {
	if len(b) == 0 {
		return 0, "", errors.New("invalid length")
	}
	l := int(b[0])
	if len(b) < l+1 {
		return 0, "", errors.New("invalid length")
	}
	s := make([]byte, l)
	copy(s, b[1:l+1])
	return l + 1, string(s), nil
}

func decodeString3(b []byte) (string, string, string, error) {
	n, s1, err := readString(b)
	if err != nil {
		return "", "", "", err
	}
	b = b[n:]
	n, s2, err := readString(b)
	if err != nil {
		return "", "", "", err
	}
	b = b[n:]
	n, s3, err := readString(b)
	if err != nil {
		return "", "", "", err
	}
	return s1, s2, s3, nil
}

func (s *Transport) encodeNetConn(v reflect.Value) ([]byte, error) {
	var conn net.Conn
	switch t := v.Interface().(type) {
	case net.TCPConn:
		conn = &t
	case net.UDPConn:
		conn = &t
	case net.Conn:
		conn = t
	default:
		return nil, errors.New("unknown type")
	}

	// Flip remote and local for encoding
	return encodeString3(conn.LocalAddr().Network(), conn.RemoteAddr().String(), conn.LocalAddr().String())
}

func (s *Transport) decodeNetConn(v reflect.Value, b []byte) error {
	network, local, remote, err := decodeString3(b)
	if err != nil {
		return err
	}
	conn, err := s.waitConn(network, local, remote, 10*time.Second)
	if err != nil {
		return err
	}

	v.Set(reflect.ValueOf(conn).Elem())

	return nil
}

func (s *Transport) initializeHandler() *codec.MsgpackHandle {
	mh := &codec.MsgpackHandle{WriteExt: true}
	mh.RawToString = true
	err := mh.AddExt(reflect.TypeOf(channel{}), 0x01, s.encodeChannel, s.decodeChannel)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(byteStream{}), 0x02, s.encodeStream, s.decodeStream)
	if err != nil {
		panic(err)
	}

	err = mh.AddExt(reflect.TypeOf(libchan.ByteStreamWrapper{}), 0x03, s.encodeWrapper, s.decodeWrapper)
	if err != nil {
		panic(err)
	}

	// Register networks
	s.networks["tcp"] = 0x04
	s.netConns[0x04] = make(map[string]net.Conn)
	err = mh.AddExt(reflect.TypeOf(net.TCPConn{}), 0x04, s.encodeNetConn, s.decodeNetConn)
	if err != nil {
		panic(err)
	}

	s.networks["udp"] = 0x05
	s.netConns[0x05] = make(map[string]net.Conn)
	err = mh.AddExt(reflect.TypeOf(net.UDPConn{}), 0x05, s.encodeNetConn, s.decodeNetConn)
	if err != nil {
		panic(err)
	}

	// TODO add unix network as 0x06

	return mh
}
