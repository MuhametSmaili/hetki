package observe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MSmaili/hetki/internal/backend/tmux/internal/format"
)

// ErrServerChanged means the sampled process or connected server changed.
var ErrServerChanged = errors.New("tmux status server incarnation changed")

// Server pins observation to one native process incarnation. Socket is a
// connection address; Identity deliberately does not depend on its spelling.
type Server struct {
	Socket  string
	PID     int
	Started uint64
	Birth   string
	Version string
}

// Identity is the stable daemon key for this server incarnation.
func (s Server) Identity() string {
	value := fmt.Sprintf("%d\x00%d\x00%s", s.PID, s.Started, s.Birth)
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

// Validate checks the native process birth without invoking a tmux subprocess.
func (s Server) Validate(ctx context.Context) error {
	if err := s.validateDescriptor(); err != nil {
		return err
	}

	birth, err := readProcessBirth(ctx, s.PID)
	if err != nil {
		return fmt.Errorf("validate tmux server process: %w", err)
	}

	if birth != s.Birth {
		return ErrServerChanged
	}

	return nil
}

// Runner executes a bounded query against the selected tmux server.
type Runner interface {
	RunLimited(context.Context, int, ...string) (string, error)
}

// ResolveServer checks the selected server's metadata and native birth before
// and after discovery. The runner must honor cancellation and its output limit.
func ResolveServer(ctx context.Context, runner Runner) (Server, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	return resolveServer(ctx, runner, readProcessBirth)
}

func resolveServer(ctx context.Context, runner Runner, birth func(context.Context, int) (string, error)) (Server, error) {
	before, err := queryServer(ctx, runner)
	if err != nil {
		return Server{}, err
	}

	before.Birth, err = birth(ctx, before.PID)
	if err != nil {
		return Server{}, fmt.Errorf("read tmux server birth: %w", err)
	}

	after, err := queryServer(ctx, runner)
	if err != nil {
		return Server{}, err
	}

	after.Birth, err = birth(ctx, after.PID)
	if err != nil {
		return Server{}, fmt.Errorf("recheck tmux server birth: %w", err)
	}

	if before != after {
		return Server{}, ErrServerChanged
	}

	if err := after.validateDescriptor(); err != nil {
		return Server{}, err
	}

	return after, ctx.Err()
}

func queryServer(ctx context.Context, runner Runner) (Server, error) {
	out, err := runner.RunLimited(ctx, 8192, "display-message", "-p", "#{q:socket_path}|#{pid}|#{start_time}|#{version}")
	if err != nil {
		return Server{}, fmt.Errorf("query tmux status server: %w", err)
	}

	fields, err := format.Fields(strings.TrimSuffix(out, "\n"), 4)
	if err != nil {
		return Server{}, fmt.Errorf("parse tmux status server: %w", err)
	}

	pid, pidErr := strconv.Atoi(fields[1])
	started, startErr := strconv.ParseUint(fields[2], 10, 64)
	if pidErr != nil || startErr != nil || pid <= 0 || pid > math.MaxInt32 || started == 0 {
		return Server{}, fmt.Errorf("invalid tmux status server process identity")
	}

	return Server{Socket: fields[0], PID: pid, Started: started, Version: fields[3]}, nil
}

func (s Server) validateDescriptor() error {
	if !filepath.IsAbs(s.Socket) || len(s.Socket) > 4096 || strings.ContainsRune(s.Socket, 0) {
		return fmt.Errorf("invalid tmux status socket")
	}

	if s.PID <= 0 || s.PID > math.MaxInt32 || s.Started == 0 || s.Birth == "" || len(s.Birth) > 128 {
		return fmt.Errorf("invalid tmux status server incarnation")
	}

	return nil
}
