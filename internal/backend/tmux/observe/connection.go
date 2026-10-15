package observe

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend/tmux/internal/format"
)

const maxControlLine = 64 << 10

// Connection owns one native session subscription, with no helper process.
// Close joins its wire reader; the caller owns and joins any ReadOutput goroutine.
type Connection struct {
	wire       *net.UnixConn
	input      *os.File
	output     *os.File
	reader     *bufio.Scanner
	layout     headerLayout
	session    string
	generation string
	done       chan struct{}
	wireErr    error
	closeOnce  sync.Once
	closeErr   error
	reading    atomic.Bool
}

// Open checks the exact server and session before returning.
// ctx owns startup only; ReadOutput and Close own the returned connection's life.
func Open(ctx context.Context, server Server, session string) (_ *Connection, err error) {
	layout, err := headerLayoutForVersion(server.Version)
	if err != nil {
		return nil, err
	}

	if err := format.ValidateObjectID("status session", session, '$'); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := server.Validate(ctx); err != nil {
		return nil, err
	}

	c, err := connectNative(ctx, server.Socket, session, layout)
	if err != nil {
		return nil, fmt.Errorf("connect native status observer: %w", err)
	}
	go c.watchWire()
	defer func() {
		if err != nil {
			err = errors.Join(err, c.Close())
			err = errors.Join(err, c.wireErr)
		}
	}()
	stop := context.AfterFunc(ctx, func() {
		c.closeResources()
	})
	defer stop()

	if err := c.confirmServer(server); err != nil {
		return nil, fmt.Errorf("confirm native status observer: %w", errors.Join(err, ctx.Err()))
	}

	if err := server.Validate(ctx); err != nil {
		return nil, err
	}

	if !stop() {
		return nil, ctx.Err()
	}

	return c, nil
}

func connectNative(ctx context.Context, socket, session string, layout headerLayout) (_ *Connection, err error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}

	c := &Connection{
		wire:    conn.(*net.UnixConn),
		layout:  layout,
		session: session,
		done:    make(chan struct{}),
	}
	defer func() {
		if err != nil {
			c.closeResources()
			err = errors.Join(err, c.closeErr)
		}
	}()

	if err := c.identify(ctx); err != nil {
		return nil, err
	}

	c.reader = bufio.NewScanner(c.output)
	c.reader.Buffer(make([]byte, 4096), maxControlLine)
	c.reader.Split(splitControlLine)
	return c, nil
}

func (c *Connection) identify(ctx context.Context) (err error) {
	input, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	c.input = writer
	defer func() {
		err = errors.Join(err, input.Close())
	}()

	reader, output, err := os.Pipe()
	if err != nil {
		return err
	}
	c.output = reader
	defer func() {
		err = errors.Join(err, output.Close())
	}()

	deadline, _ := ctx.Deadline()
	if err := c.wire.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = c.wire.Close()
	})
	defer stop()

	if err := identifyNative(c.wire, c.layout, c.session, input, output); err != nil {
		return errors.Join(err, ctx.Err())
	}

	if !stop() {
		return ctx.Err()
	}

	return c.wire.SetDeadline(time.Time{})
}

func (c *Connection) watchWire() {
	defer close(c.done)
	defer c.closeResources()

	for {
		_, err := readNativeMessage(c.wire, c.layout)
		if err != nil {
			c.wireErr = err
			return
		}
	}
}

func (c *Connection) confirmServer(server Server) error {
	// Control clients do not receive MSG_READY on attach. An ordered command
	// reply proves attachment and checks the server reached through the socket.
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	c.generation = hex.EncodeToString(nonce[:])
	marker := "hetki-status-" + c.generation
	command := "display-message -p '" + marker + "|#{pid}|#{start_time}|#{version}|#{session_id}'\n"
	if _, err := io.WriteString(c.input, command); err != nil {
		return err
	}

	expected := fmt.Sprintf("%s|%d|%d|%s|%s", marker, server.PID, server.Started, server.Version, c.session)
	return c.readConfirmation(marker, expected)
}

// Generation changes for every connection, even to the same session/server.
func (c *Connection) Generation() string {
	return c.generation
}

// Close is idempotent and interrupts blocked native/output reads.
func (c *Connection) Close() error {
	c.closeResources()
	<-c.done
	return c.closeErr
}

func (c *Connection) closeResources() {
	c.closeOnce.Do(func() {
		c.closeErr = c.wire.Close()
		if c.input != nil {
			c.closeErr = errors.Join(c.closeErr, c.input.Close())
		}
		if c.output != nil {
			c.closeErr = errors.Join(c.closeErr, c.output.Close())
		}
	})
}
