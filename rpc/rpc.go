package rpc

import (
	"errors"
	"io"
	"log"
	"reflect"

	"github.com/docker/libchan"
)

type callMsg struct {
	Name string
	Args []interface{}
	Ret  libchan.Sender
}

type returnMsg struct {
	Error string
	Out   []interface{}
}

func CallAsync(s libchan.Sender, name string, args ...interface{}) error {
	f := &callMsg{
		Name: name,
		Args: args,
	}
	return s.Send(f)
}

func Call(s libchan.Sender, name string, args ...interface{}) ([]interface{}, error) {
	f := &callMsg{
		Name: name,
		Args: args,
	}
	var r libchan.Receiver
	r, f.Ret = libchan.Pipe()
	if err := s.Send(f); err != nil {
		return nil, err
	}
	var ret returnMsg
	if err := r.Receive(&ret); err != nil {
		return nil, err
	}
	if ret.Error != "" {
		log.Printf("Returned error: %s", ret.Error)
		return nil, errors.New(ret.Error)
	}
	return ret.Out, nil
}

type Server struct {
	receiver libchan.Receiver
	methods  map[string]reflect.Value
}

func NewServer(r libchan.Receiver, t reflect.Type, imp reflect.Value) *Server {
	if t.Kind() != reflect.Interface {
		panic("invalid interface value")
	}
	if !imp.Type().Implements(t) {
		panic("value does not implement interface")
	}
	methods := make(map[string]reflect.Value)
	s := &Server{
		receiver: r,
		methods:  methods,
	}

	for i := 0; i < t.NumMethod(); i++ {
		method := t.Method(i)
		methods[method.Name] = imp.MethodByName(method.Name)
	}

	return s
}

type Decodable interface {
	Decode(...interface{}) error
}

func (s *Server) Serve() error {
	for {
		var r callMsg
		if err := s.receiver.Receive(&r); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		f, ok := s.methods[r.Name]
		if !ok {
			log.Printf("Method does not exist: %s", r.Name)
			RespondError(r.Ret, "method does not exist")
			continue
		}
		ft := f.Type()
		if len(r.Args) != ft.NumIn() {
			RespondError(r.Ret, "invalid number of arguments")
			continue
		}
		callArgs := make([]reflect.Value, ft.NumIn())
		for i, arg := range r.Args {
			v := reflect.ValueOf(arg)
			it := ft.In(i)
			if v.Type().Kind() == it.Kind() {
				callArgs[i] = v
			} else if d, ok := arg.(Decodable); ok {
				vp := reflect.New(it)
				if err := d.Decode(vp); err != nil {
					RespondError(r.Ret, "decode error: "+err.Error())
					continue
				}
				callArgs[i] = vp.Elem()
			} else if v.Type().ConvertibleTo(it) {
				callArgs[i] = v.Convert(it)
			} else {
				RespondError(r.Ret, "unexpected argument type")
				continue
			}
		}
		outValues := f.Call(callArgs)
		if r.Ret == nil {
			if len(outValues) > 0 {
				log.Printf("Ignored returned values")
			}
			continue
		}
		out := make([]interface{}, len(outValues))
		for i, v := range outValues {
			out[i] = v.Interface()
		}
		if err := r.Ret.Send(&returnMsg{Out: out}); err != nil {
			log.Printf("Error sending response: %s", err)
		}
	}
	return nil
}

func RespondError(s libchan.Sender, msg string) {
	if s != nil {
		if err := s.Send(&returnMsg{Error: msg}); err != nil {
			log.Printf("Error sending response: %s", err)
		}
		if err := s.Close(); err != nil {
			log.Printf("Error closing response: %s", err)
		}
	}
}
