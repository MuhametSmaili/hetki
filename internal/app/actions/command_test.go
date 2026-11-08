package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionCommandHelper(t *testing.T) {
	index := slices.Index(os.Args, "--action-helper")
	if index < 0 {
		return
	}
	args := os.Args[index+1:]
	var err error
	switch args[0] {
	case "hold":
		err = actionHelperHold(args)
	case "descendant", "orphan":
		err = actionHelperSpawn(args)
	case "orphan-output":
		err = actionHelperSpawn(args)
		if err == nil {
			_, _ = fmt.Fprintln(os.Stdout, `{"id":"partial","label":"Partial"}`)
			err = recordActionAttempt(args[2])
		}
	case "flood":
		_, _ = io.Copy(os.Stdout, strings.NewReader(strings.Repeat("x", maxOutput+1024)))
	case "args":
		for _, arg := range args[1:] {
			_, _ = fmt.Fprintln(os.Stdout, arg)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func recordActionAttempt(path string) (err error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	_, err = file.WriteString("x")
	return err
}

func actionHelperHold(args []string) error {
	conn, err := net.Dial("tcp", args[1])
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ready")); err != nil {
		return err
	}
	if len(args) > 2 {
		ready := os.NewFile(3, "ready")
		_, err := ready.Write([]byte("!"))
		_ = ready.Close()
		if err != nil {
			return err
		}
	}
	_, err = io.Copy(io.Discard, conn)
	return err
}

func actionHelperSpawn(args []string) error {
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	defer read.Close()
	defer write.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestActionCommandHelper$", "--", "--action-helper", "hold", args[1], "notify")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.ExtraFiles = []*os.File{write}
	if err := cmd.Start(); err != nil {
		return err
	}
	if args[0] == "descendant" {
		return cmd.Wait()
	}
	_, err = io.ReadFull(read, make([]byte, 1))
	return err
}

func helperCommand(t *testing.T, args ...string) Command {
	t.Helper()
	bin, err := os.Executable()
	require.NoError(t, err)
	return Command{Argv: append([]string{bin, "-test.run=^TestActionCommandHelper$", "--", "--action-helper"}, args...)}
}

func TestCommandPreservesLiteralArguments(t *testing.T) {
	command := helperCommand(t, "args", "two words; $(exit 99)", "quote'\"")

	result, err := runCommand(t.Context(), command, Request{Version: 1})

	require.NoError(t, err)
	assert.Equal(t, "two words; $(exit 99)\nquote'\"\n", string(result.output))
	assert.True(t, result.exitedSuccessfully)
}

func TestCommandBoundsOutputThroughIOCopy(t *testing.T) {
	command := helperCommand(t, "flood")

	result, err := runCommand(t.Context(), command, Request{})

	require.ErrorIs(t, err, ErrOutputLimit)
	assert.Nil(t, result.output)
}

func TestCommandCancellationRetiresOwnedProcesses(t *testing.T) {
	for _, mode := range []string{"hold", "descendant", "orphan"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			defer listener.Close()
			require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			command := helperCommand(t, mode, listener.Addr().String())
			done := make(chan error, 1)
			go func() {
				_, runErr := runCommand(ctx, command, Request{})
				done <- runErr
			}()
			defer func() {
				cancel()
				select {
				case runErr := <-done:
					if mode == "orphan" {
						require.ErrorIs(t, runErr, exec.ErrWaitDelay)
					} else {
						require.ErrorIs(t, runErr, context.Canceled)
					}
				case <-time.After(5 * time.Second):
					t.Error("command did not finish")
				}
			}()
			conn, err := listener.Accept()
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
			ready := make([]byte, 5)
			_, err = io.ReadFull(conn, ready)
			require.NoError(t, err)
			require.True(t, bytes.Equal(ready, []byte("ready")))

			if mode != "orphan" {
				cancel()
			}
			_, err = conn.Read(make([]byte, 1))

			require.ErrorIs(t, err, io.EOF)
		})
	}
}
