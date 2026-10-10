package observe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/MSmaili/hetki/internal/backend/tmux/internal/format"
)

// ErrStreamLost requires invalidating this connection's evidence. No replay
// or continuity across reconnects is promised.
var ErrStreamLost = errors.New("tmux status output continuity lost")

// PaneOutput attributes bytes to a pane, not to an authenticated writer process.
// Data is borrowed until the synchronous callback returns.
type PaneOutput struct {
	Pane string
	Data []byte
}

// ReadOutput delivers ordered decoded tmux output, leaving terminal framing and
// protocol interpretation to its caller. Call once; callbacks must remain bounded.
// changed is optional and reports topology hints, never authoritative pane facts.
func (c *Connection) ReadOutput(ctx context.Context, emit func(PaneOutput) error, changed func()) (err error) {
	if emit == nil || !c.reading.CompareAndSwap(false, true) {
		return fmt.Errorf("status output requires one reader and a callback")
	}

	stop := context.AfterFunc(ctx, func() {
		c.closeResources()
	})
	defer stop()
	defer func() {
		closeErr := c.Close()
		if errors.Is(err, ErrStreamLost) && ctx.Err() == nil {
			err = errors.Join(err, c.wireErr)
		}
		err = errors.Join(err, closeErr, ctx.Err())
	}()

	for {
		if err := c.checkRead(ctx); err != nil {
			return err
		}

		line, readErr := c.readLine()
		if readErr != nil {
			return errors.Join(ErrStreamLost, readErr)
		}

		if err := c.deliverLine(line, emit, changed); err != nil {
			return err
		}
	}
}

func (c *Connection) checkRead(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case <-c.done:
		return errors.Join(ErrStreamLost, c.wireErr)
	default:
		return nil
	}
}

func (c *Connection) deliverLine(line []byte, emit func(PaneOutput) error, changed func()) error {
	if err := checkControlNotice(line, c.session); err != nil {
		return err
	}

	if !bytes.HasPrefix(line, []byte("%output ")) {
		if changed != nil && topologyNotice(line) {
			changed()
		}
		return nil
	}

	output, err := parsePaneOutput(line[len("%output "):])
	if err != nil {
		return errors.Join(ErrStreamLost, err)
	}

	if err := emit(output); err != nil {
		return fmt.Errorf("consume tmux status output: %w", err)
	}

	return nil
}

func topologyNotice(line []byte) bool {
	name, _, _ := bytes.Cut(line, []byte(" "))
	switch string(name) {
	case "%layout-change", "%window-add", "%window-close", "%unlinked-window-add",
		"%unlinked-window-close", "%sessions-changed", "%session-window-changed",
		"%window-pane-changed", "%pane-mode-changed":
		return true
	default:
		return false
	}
}

func (c *Connection) readLine() ([]byte, error) {
	if c.reader.Scan() {
		return c.reader.Bytes(), nil
	}

	if err := c.reader.Err(); err != nil {
		return nil, fmt.Errorf("read bounded tmux control line: %w", err)
	}

	return nil, io.EOF
}

func splitControlLine(data []byte, atEOF bool) (int, []byte, error) {
	if index := bytes.IndexByte(data, '\n'); index >= 0 {
		return index + 1, data[:index], nil
	}

	if atEOF && len(data) > 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}

	return 0, nil, nil
}

func (c *Connection) readConfirmation(marker, expected string) error {
	var detail string
	for {
		line, err := c.readLine()
		if err != nil {
			return err
		}

		if err := checkControlNotice(line, c.session); err != nil {
			return fmt.Errorf("%w (last server reply %q)", err, detail)
		}

		if bytes.HasPrefix(line, []byte(marker+"|")) {
			if string(line) != expected {
				return ErrServerChanged
			}
			return nil
		}

		if len(line) > 0 && line[0] != '%' {
			detail = string(line[:min(len(line), 256)])
		}
	}
}

func checkControlNotice(line []byte, session string) error {
	name, rest, _ := bytes.Cut(line, []byte(" "))
	switch string(name) {
	case "%exit", "%pause", "%continue", "%extended-output", "%error":
		return fmt.Errorf("%w: %s", ErrStreamLost, name)
	case "%session-changed":
		attached, _, _ := bytes.Cut(rest, []byte(" "))
		if string(attached) != session {
			return fmt.Errorf("%w: attached session changed", ErrStreamLost)
		}
	}

	return nil
}

func parsePaneOutput(line []byte) (PaneOutput, error) {
	pane, escaped, ok := bytes.Cut(line, []byte(" "))
	if !ok {
		return PaneOutput{}, fmt.Errorf("missing tmux output pane")
	}

	id := string(pane)
	if err := format.ValidateObjectID("output pane", id, '%'); err != nil {
		return PaneOutput{}, err
	}

	data, err := unescapeOutput(escaped)
	if err != nil {
		return PaneOutput{}, err
	}

	return PaneOutput{Pane: id, Data: data}, nil
}

func unescapeOutput(data []byte) ([]byte, error) {
	written := 0
	for i := 0; i < len(data); i++ {
		value := data[i]
		if value == '\\' {
			if i+3 >= len(data) {
				return nil, fmt.Errorf("incomplete tmux octal escape")
			}
			decoded, err := decodeOctal(data[i+1 : i+4])
			if err != nil {
				return nil, err
			}
			value = decoded
			i += 3
		}
		data[written] = value
		written++
	}

	return data[:written], nil
}

func decodeOctal(digits []byte) (byte, error) {
	value := 0
	for _, digit := range digits {
		if digit < '0' || digit > '7' {
			return 0, fmt.Errorf("invalid tmux octal digit")
		}
		value = value*8 + int(digit-'0')
	}

	if value > 255 {
		return 0, fmt.Errorf("tmux octal escape exceeds a byte")
	}

	return byte(value), nil
}
