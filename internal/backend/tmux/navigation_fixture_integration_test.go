//go:build integration

package tmux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/observe"
	"github.com/stretchr/testify/require"
)

type navigationFixture struct {
	backend     *TmuxBackend
	ctx         context.Context
	source      backend.Target
	destination backend.Target
}

// Mutate the private server after discovery, then leave dispatch to real tmux.
type navigationDispatchClient struct {
	Client
	before func()
}

func (c navigationDispatchClient) Execute(ctx context.Context, action Action) error {
	c.before()
	return c.Client.Execute(ctx, action)
}

func newNavigationFixture(t *testing.T) navigationFixture {
	t.Helper()
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "")
	t.Setenv("BASH_ENV", "")
	b := newIsolatedTmuxBackend(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	fixture := navigationFixture{backend: b, ctx: ctx}
	create := func(name string) backend.Target {
		output := fixture.run(t, "new-session", "-d", "-s", name, "-P", "-F",
			"#{session_id}|#{window_id}|#{pane_id}", "/bin/sh", "-c", "exec sleep 60")
		fields := strings.Split(output, "|")
		require.Len(t, fields, 3)
		return backend.Target{SessionID: fields[0], WindowID: fields[1], PaneID: fields[2]}
	}
	fixture.source = create("source")
	fixture.destination = create("destination:with-colon")
	socket := fixture.run(t, "display-message", "-p", "#{socket_path}")
	pid := fixture.run(t, "display-message", "-p", "#{pid}")
	t.Setenv("TMUX", socket+","+pid+","+strings.TrimPrefix(fixture.source.SessionID, "$"))
	t.Setenv("TMUX_PANE", fixture.source.PaneID)
	return fixture
}

func (f navigationFixture) run(t *testing.T, args ...string) string {
	t.Helper()
	output, err := f.backend.client.Run(f.ctx, args...)
	require.NoError(t, err, "tmux %v", args)
	return strings.TrimSpace(output)
}

func (f navigationFixture) destinationAt(t *testing.T, level string) backend.Target {
	t.Helper()
	target := f.destination
	if level != "window" && level != "pane" && level != "linked pane" {
		return backend.Target{SessionID: target.SessionID}
	}

	target.WindowID = f.run(t, "new-window", "-d", "-t", target.SessionID+":", "-P", "-F", "#{window_id}", "/bin/sh", "-c", "exec sleep 60")
	target.PaneID = ""
	if level != "window" {
		target.PaneID = f.run(t, "split-window", "-d", "-t", target.WindowID, "-P", "-F", "#{pane_id}", "/bin/sh", "-c", "exec sleep 60")
	}
	if level == "linked pane" {
		target.SessionID = f.run(t, "new-session", "-d", "-s", "linked", "-P", "-F", "#{session_id}", "/bin/sh", "-c", "exec sleep 60")
		f.run(t, "link-window", "-s", target.WindowID, "-t", target.SessionID+":4")
	}
	return target
}

func (f navigationFixture) attach(t *testing.T, session string) string {
	t.Helper()
	cmd := exec.CommandContext(f.ctx, f.backend.client.(*client).bin, "-C", "attach-session", "-t", session)
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	cmd.Cancel = func() error {
		err := input.Close()
		if errors.Is(err, os.ErrClosed) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = io.Discard, &stderr
	started := false
	t.Cleanup(func() {
		if err := input.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Errorf("close navigation client input: %v", err)
		}
		if started {
			if err := cmd.Wait(); err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("join navigation client: %v: %s", err, stderr.String())
			}
		}
	})
	require.NoError(t, cmd.Start())
	started = true
	ready := fmt.Sprintf("navigation-client-ready-%d", cmd.Process.Pid)
	_, err = fmt.Fprintf(input, "wait-for -S %s\n", ready)
	require.NoError(t, err)
	f.run(t, "wait-for", ready)

	prefix := fmt.Sprintf("%d|", cmd.Process.Pid)
	for line := range strings.SplitSeq(f.run(t, "list-clients", "-F", "#{client_pid}|#{client_name}"), "\n") {
		if name, found := strings.CutPrefix(line, prefix); found {
			return name
		}
	}
	t.Fatal("attached navigation client was not listed")
	return ""
}

func (f navigationFixture) clientTarget(t *testing.T, name string) backend.Target {
	t.Helper()
	for line := range strings.SplitSeq(f.run(t, "list-clients", "-F", "#{client_name}|#{session_id}|#{window_id}|#{pane_id}"), "\n") {
		fields := strings.Split(line, "|")
		require.Len(t, fields, 4)
		if fields[0] == name {
			return backend.Target{SessionID: fields[1], WindowID: fields[2], PaneID: fields[3]}
		}
	}
	t.Fatalf("navigation client %q was not listed", name)
	return backend.Target{}
}

func (f navigationFixture) observe(t *testing.T) (*observe.Connection, string) {
	t.Helper()
	server, err := f.backend.StatusServer(f.ctx)
	require.NoError(t, err)
	connection, err := observe.Open(f.ctx, server, f.source.SessionID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	for name := range strings.SplitSeq(f.run(t, "list-clients", "-F", "#{client_name}"), "\n") {
		if strings.HasPrefix(name, observe.ClientNamePrefix) {
			return connection, name
		}
	}
	t.Fatal("status observer was not listed")
	return nil, ""
}
