package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrScreenTooLarge means one pane's screen exceeded the per-screen byte limit.
// Only that pane's capture fails.
var ErrScreenTooLarge = fmt.Errorf("tmux screen exceeds %d bytes", maxScreenBytes)

const (
	maxCaptureTargets    = 128
	maxCaptureRows       = 200
	maxCaptureColumns    = 1000
	maxScreenBytes       = 256 << 10
	maxCaptureBytes      = 4 << 20
	maxCaptureBatchBytes = maxCaptureBytes + maxInventoryBytes
)

// Region selects visible rows, never scrollback. Zero means the whole viewport.
// Bottom selects the last Rows rows (or the whole viewport if shorter); otherwise
// Start is a zero-based row and Rows must fit without truncation.
type Region struct {
	Start  int
	Rows   int
	Bottom bool
}

func (r Region) bounds(height int) (int, int, error) {
	if r.Start < 0 || r.Start >= height || r.Rows < 0 || r.Rows > maxCaptureRows {
		return 0, 0, fmt.Errorf("invalid capture region")
	}
	if r.Bottom {
		if r.Start != 0 || r.Rows == 0 {
			return 0, 0, fmt.Errorf("bottom capture requires a row count and no start offset")
		}
		return max(0, height-r.Rows), height - 1, nil
	}
	if r.Rows == 0 {
		if r.Start != 0 || height > maxCaptureRows {
			return 0, 0, fmt.Errorf("whole viewport exceeds %d rows", maxCaptureRows)
		}
		return 0, height - 1, nil
	}
	if r.Rows > height-r.Start {
		return 0, 0, fmt.Errorf("capture region exceeds viewport")
	}
	return r.Start, r.Start + r.Rows - 1, nil
}

// CaptureTarget is an immutable copy of a validated inventory row and region.
// Revision lets the coordinator bind its service generation/sequence before I/O;
// a successful native capture still requires that final service-side check.
type CaptureTarget struct {
	server   string
	revision uint64
	pane     Pane
	first    int
	last     int
}

func (t CaptureTarget) Pane() Pane       { return t.pane }
func (t CaptureTarget) Revision() uint64 { return t.revision }

// Target binds a screen region to stable live facts from this inventory.
func (i Inventory) Target(id string, region Region) (CaptureTarget, error) {
	pane, found := i.panes[id]
	if !found || i.revision == 0 {
		return CaptureTarget{}, fmt.Errorf("capture requires an inventory pane")
	}

	if err := i.problems[id]; err != nil {
		return CaptureTarget{}, fmt.Errorf("pane %s is unavailable: %w", id, err)
	}

	if pane.Dead || pane.Root.Identity() == "" || pane.Foreground.Identity() == "" {
		return CaptureTarget{}, fmt.Errorf("pane %s has no live validated owner", id)
	}

	if pane.Columns > maxCaptureColumns {
		return CaptureTarget{}, fmt.Errorf("capture exceeds %d columns", maxCaptureColumns)
	}

	first, last, err := region.bounds(pane.Rows)
	if err != nil {
		return CaptureTarget{}, err
	}
	return CaptureTarget{
		server:   i.server,
		revision: i.revision,
		pane:     pane,
		first:    first,
		last:     last,
	}, nil
}

// Screen owns its plain text. Err is non-nil and Text empty when a target changed
// or became unavailable. StartedAt conservatively dates acquisition, not receipt.
type Screen struct {
	Target    CaptureTarget
	Text      string
	StartedAt time.Time
	Err       error
}

// Capture reads the screens of mixed-application targets in one tmux request.
// The same request reads the pane facts after the capture, and OS process
// checks follow it. A screen is kept only when those facts still match the
// target's inventory. It retains no text after it returns. A pane that no
// longer exists fails only its own screen; any other failure of the whole
// request discards every screen. Per-pane changes are in Screen.Err.
func (s *Sampler) Capture(ctx context.Context, targets []CaptureTarget) ([]Screen, error) {
	ids, err := s.captureIDs(targets)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(ctx, acquisitionBudget)
	defer cancel()

	started := time.Now()
	texts, after, err := s.captureBatch(ctx, targets)
	if err != nil {
		return s.captureSurvivors(ctx, targets, err)
	}

	return finishCaptures(targets, texts, after, started), ctx.Err()
}

// captureSurvivors retries once without targets whose panes no longer exist.
// tmux stops a command sequence at its first error, so a pane closed after the
// inventory would otherwise discard the screens of every other target.
func (s *Sampler) captureSurvivors(ctx context.Context, targets []CaptureTarget, batchErr error) ([]Screen, error) {
	if ctx.Err() != nil || errors.Is(batchErr, ErrServerChanged) {
		return nil, batchErr
	}

	current, err := s.queryInventory(ctx)
	if err != nil {
		return nil, errors.Join(batchErr, err)
	}

	survivors := make([]CaptureTarget, 0, len(targets))
	for _, target := range targets {
		if _, found := current.panes[target.pane.ID]; found {
			survivors = append(survivors, target)
		}
	}
	if len(survivors) == len(targets) {
		return nil, batchErr
	}

	// Targets missing from the post-capture facts fail with ErrChanged.
	var texts map[string]capturedText
	var after Inventory
	started := time.Now()
	if len(survivors) > 0 {
		texts, after, err = s.captureBatch(ctx, survivors)
		if err != nil {
			return nil, err
		}
	}

	return finishCaptures(targets, texts, after, started), ctx.Err()
}

func (s *Sampler) captureIDs(targets []CaptureTarget) ([]string, error) {
	if len(targets) > maxCaptureTargets {
		return nil, fmt.Errorf("capture exceeds %d targets", maxCaptureTargets)
	}
	ids := make([]string, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	server := s.server.Identity()
	for _, target := range targets {
		if target.revision == 0 || target.server != server {
			return nil, fmt.Errorf("capture target belongs to another server or is uninitialized")
		}
		if seen[target.pane.ID] {
			return nil, fmt.Errorf("duplicate capture target %s", target.pane.ID)
		}
		seen[target.pane.ID] = true
		ids = append(ids, target.pane.ID)
	}
	return ids, nil
}

func (t CaptureTarget) check(i Inventory) error {
	if i.problems[t.pane.ID] != nil || i.panes[t.pane.ID] != t.pane {
		return errors.Join(ErrChanged, i.problems[t.pane.ID])
	}
	return nil
}

// capturedText is one pane's screen, or why it could not be kept.
type capturedText struct {
	text string
	err  error
}

func finishCaptures(targets []CaptureTarget, texts map[string]capturedText, after Inventory, started time.Time) []Screen {
	results := make([]Screen, len(targets))
	for n, target := range targets {
		captured := texts[target.pane.ID]
		result := Screen{Target: target, Err: errors.Join(target.check(after), captured.err)}
		if result.Err == nil {
			result.Text = captured.text
			result.StartedAt = started
		}
		results[n] = result
	}
	return results
}

func (s *Sampler) captureBatch(ctx context.Context, targets []CaptureTarget) (map[string]capturedText, Inventory, error) {
	processes := processSample{read: s.process, cache: make(map[int]processResult)}
	if err := s.validateProcess(ctx, &processes); err != nil {
		return nil, Inventory{}, err
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, Inventory{}, err
	}
	marker := "hetki-capture-" + hex.EncodeToString(nonce[:])
	args, ids := captureBatchArgs(targets, marker)
	out, err := s.run(ctx, maxCaptureBatchBytes, args...)
	if err != nil {
		return nil, Inventory{}, fmt.Errorf("capture tmux batch: %w", err)
	}
	texts, after, err := s.parseCaptureBatch(out, marker, targets, ids)
	if err != nil {
		return nil, Inventory{}, err
	}
	after, err = s.sampleProcesses(ctx, after, &processes)
	if err != nil {
		return nil, Inventory{}, err
	}
	return texts, after, nil
}

func captureBatchArgs(targets []CaptureTarget, marker string) ([]string, []string) {
	args := []string{"display-message", "-p", "#{pid}|#{start_time}|#{version}"}
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.pane.ID)
		key := strings.TrimPrefix(target.pane.ID, "%")
		args = appendCommand(args, "display-message", "-p", marker+"-begin-"+key)
		args = appendCommand(args, "capture-pane", "-p", "-N", "-S", strconv.Itoa(target.first), "-E", strconv.Itoa(target.last), "-t", target.pane.ID)
		args = appendCommand(args, "display-message", "-p", marker+"-end-"+key)
	}
	args = appendCommand(args, "display-message", "-p", marker+"-facts")
	args = appendCommand(args, "display-message", "-p", "#{pid}|#{start_time}|#{version}")
	for _, id := range ids {
		args = appendCommand(args, "display-message", "-p", "-t", id, inventoryFormat)
	}
	return args, ids
}

func (s *Sampler) parseCaptureBatch(out, marker string, targets []CaptureTarget, ids []string) (map[string]capturedText, Inventory, error) {
	screens, facts, ok := strings.Cut(out, marker+"-facts\n")
	if !ok || len(screens) > maxCaptureBytes || len(facts) > maxInventoryBytes {
		return nil, Inventory{}, fmt.Errorf("missing or oversized capture batch section")
	}
	header, rest, ok := strings.Cut(screens, "\n")
	if !ok || header != fmt.Sprintf("%d|%d|%s", s.server.PID, s.server.Started, s.server.Version) {
		return nil, Inventory{}, ErrServerChanged
	}
	after, err := s.parseInventory(facts, ids)
	if err != nil {
		return nil, Inventory{}, fmt.Errorf("parse post-capture inventory: %w", err)
	}
	texts, err := splitScreens(rest, marker, targets, after)
	if err != nil {
		return nil, Inventory{}, err
	}
	return texts, after, nil
}

func splitScreens(out, marker string, targets []CaptureTarget, after Inventory) (map[string]capturedText, error) {
	texts := make(map[string]capturedText, len(targets))
	for _, target := range targets {
		key := strings.TrimPrefix(target.pane.ID, "%")
		start := marker + "-begin-" + key + "\n"
		if !strings.HasPrefix(out, start) {
			return nil, fmt.Errorf("missing capture start boundary")
		}
		out = out[len(start):]
		end := marker + "-end-" + key + "\n"
		text, rest, ok := strings.Cut(out, end)
		rows := strings.Count(text, "\n")
		if !ok || !strings.HasSuffix(text, "\n") || strings.Contains(text, marker) || rows > maxCaptureRows {
			return nil, fmt.Errorf("invalid capture boundary or row count")
		}
		out = rest

		if len(text) > maxScreenBytes {
			texts[target.pane.ID] = capturedText{err: ErrScreenTooLarge}
			continue
		}
		if after.panes[target.pane.ID].Rows != target.pane.Rows {
			continue
		}
		if rows != target.last-target.first+1 {
			return nil, fmt.Errorf("unexpected capture row count for pane %s", target.pane.ID)
		}
		texts[target.pane.ID] = capturedText{text: strings.Clone(text)}
	}
	if out != "" {
		return nil, fmt.Errorf("unexpected trailing capture data")
	}
	return texts, nil
}
