package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"syscall"

	"github.com/docker/libchan"
	"github.com/docker/libchan/spdy"
)

type RemoteCommand struct {
	Cmd        string
	Args       []string
	Stdin      io.Reader
	Stdout     io.WriteCloser
	Stderr     io.WriteCloser
	StatusChan libchan.Sender
}

type CommandResponse struct {
	Status int
}

func main() {
	listener, err := net.Listen("tcp", "localhost:9323")
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	tl, err := spdy.NewTransportListener(listener, spdy.NoAuthenticator)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	for {
		t, err := tl.AcceptTransport()
		if err != nil {
			fmt.Println(err)
			break
		}

		go func() {
			for {
				receiver, err := t.WaitReceiveChannel()
				if err != nil {
					fmt.Println(err)
					break
				}

				go func() {
					for {
						command := &RemoteCommand{}
						err := receiver.Receive(command)
						if err != nil {
							fmt.Println(err)
							break
						}

						cmd := exec.Command(command.Cmd, command.Args...)

						stdin, err := cmd.StdinPipe()
						if err != nil {
							fmt.Println(err)
							break
						}
						stdout, err := cmd.StdoutPipe()
						if err != nil {
							fmt.Println(err)
							break
						}
						stderr, err := cmd.StderrPipe()
						if err != nil {
							fmt.Println(err)
							break
						}

						go func() {
							io.Copy(stdin, command.Stdin)
							stdin.Close()
						}()

						go func() {
							io.Copy(command.Stdout, stdout)
							command.Stdout.Close()
						}()

						go func() {
							io.Copy(command.Stderr, stderr)
							command.Stderr.Close()
						}()

						res := cmd.Run()
						returnResult := &CommandResponse{}
						if res != nil {
							if exiterr, ok := res.(*exec.ExitError); ok {
								returnResult.Status = exiterr.Sys().(syscall.WaitStatus).ExitStatus()
							} else {
								fmt.Println(res)
								returnResult.Status = 10
							}
						}

						err = command.StatusChan.Send(returnResult)
						if err != nil {
							fmt.Println(err)
						}
					}
				}()
			}
		}()
	}
}
