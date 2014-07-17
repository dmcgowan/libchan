package main

import (
	"fmt"
	"io"
	"net"
	"os"

	"github.com/docker/libchan"
	"github.com/docker/libchan/spdy"
)

type RemoteCommand struct {
	Cmd        string
	Args       []string
	Stdin      io.Writer
	Stdout     io.Reader
	Stderr     io.Reader
	StatusChan libchan.Sender
}

type CommandResponse struct {
	Status int
}

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("usage: <command> [<arg> ]")
		os.Exit(1)
	}

	client, err := net.Dial("tcp", "127.0.0.1:9323")
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}
	transport, err := spdy.NewClientTransport(client)
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}
	sender, err := transport.NewSendChannel()
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}

	receiver, remoteSender, err := sender.CreateNestedReceiver()
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}

	stdin, err := sender.CreateByteStream()
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}
	go func() {
		io.Copy(stdin, os.Stdin)
		stdin.Close()
	}()

	stdout, err := sender.CreateByteStream()
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}
	go io.Copy(os.Stdout, stdout)

	stderr, err := sender.CreateByteStream()
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}
	go io.Copy(os.Stderr, stderr)

	command := &RemoteCommand{
		Cmd:        os.Args[1],
		Args:       os.Args[2:],
		Stdin:      stdin,
		Stdout:     stdout,
		Stderr:     stderr,
		StatusChan: remoteSender,
	}

	err = sender.Send(command)
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}

	response := &CommandResponse{}
	err = receiver.Receive(response)
	if err != nil {
		fmt.Print(err)
		os.Exit(2)
	}
	os.Exit(response.Status)
}
