package observe

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/MSmaili/hetki/internal/backend/tmux/internal/format"
)

const inventoryFormat = "#{session_id}|#{window_id}|#{pane_id}|#{pane_pid}|#{pane_dead}|#{q:pane_tty}|#{q:pane_current_command}|#{pane_width}|#{pane_height}|#{alternate_on}"

func (s *Sampler) queryInventory(ctx context.Context, ids []string) (Inventory, error) {
	args := []string{"display-message", "-p", "#{pid}|#{start_time}|#{version}"}
	if ids == nil {
		args = appendCommand(args, "list-panes", "-a", "-F", inventoryFormat)
	} else {
		for _, id := range ids {
			args = appendCommand(args, "display-message", "-p", "-t", id, inventoryFormat)
		}
	}
	out, err := s.run(ctx, maxInventoryBytes, args...)
	if err != nil {
		return Inventory{}, fmt.Errorf("query tmux inventory: %w", err)
	}
	return s.parseInventory(out, ids)
}

func (s *Sampler) parseInventory(out string, ids []string) (Inventory, error) {
	header, rows, ok := strings.Cut(out, "\n")
	expected := fmt.Sprintf("%d|%d|%s", s.server.PID, s.server.Started, s.server.Version)
	if !ok || header != expected {
		return Inventory{}, ErrServerChanged
	}
	i := Inventory{
		server:   s.server.Identity(),
		panes:    make(map[string]Pane),
		problems: make(map[string]error),
	}
	seen := make(map[Location]bool)
	for rows != "" {
		line, rest, terminated := strings.Cut(rows, "\n")
		if !terminated || len(i.locations) >= maxLocations {
			return Inventory{}, fmt.Errorf("truncated or oversized tmux inventory")
		}
		pane, location, err := parseInventoryRow(line)
		if err != nil {
			return Inventory{}, err
		}
		if err := addInventoryPane(&i, seen, pane, location); err != nil {
			return Inventory{}, err
		}
		rows = rest
	}
	if err := checkRequestedPanes(i, ids); err != nil {
		return Inventory{}, err
	}
	return i, nil
}

func addInventoryPane(i *Inventory, seen map[Location]bool, pane Pane, location Location) error {
	if previous, exists := i.panes[pane.ID]; exists && previous != pane {
		return fmt.Errorf("%w: conflicting linked pane %s", ErrChanged, pane.ID)
	}
	if seen[location] {
		return fmt.Errorf("duplicate tmux pane location")
	}
	i.panes[pane.ID] = pane
	if len(i.panes) > maxInventoryPanes {
		return fmt.Errorf("tmux inventory exceeds %d panes", maxInventoryPanes)
	}
	seen[location] = true
	i.locations = append(i.locations, location)
	return nil
}

func checkRequestedPanes(i Inventory, ids []string) error {
	if ids == nil {
		return nil
	}
	if len(i.locations) != len(ids) {
		return fmt.Errorf("incomplete requested tmux inventory")
	}
	for n, id := range ids {
		if i.locations[n].Pane != id {
			return fmt.Errorf("unexpected requested tmux pane")
		}
	}
	return nil
}

func parseInventoryRow(line string) (Pane, Location, error) {
	if len(line) > 1024 {
		return Pane{}, Location{}, fmt.Errorf("tmux inventory row exceeds limit")
	}
	fields, err := format.Fields(line, 10)
	if err != nil {
		return Pane{}, Location{}, fmt.Errorf("parse tmux inventory: %w", err)
	}
	location := Location{Session: fields[0], Window: fields[1], Pane: fields[2]}
	for _, id := range []struct {
		value  string
		prefix byte
	}{{fields[0], '$'}, {fields[1], '@'}, {fields[2], '%'}} {
		if len(id.value) > 32 {
			return Pane{}, Location{}, fmt.Errorf("tmux object ID exceeds limit")
		}
		if err := format.ValidateObjectID("inventory", id.value, id.prefix); err != nil {
			return Pane{}, Location{}, err
		}
	}
	pane, err := parsePaneFacts(fields)
	return pane, location, err
}

func parsePaneFacts(fields []string) (Pane, error) {
	pid, pidErr := parseRootPID(fields[3], fields[4] == "1")
	width, widthErr := strconv.ParseUint(fields[7], 10, 16)
	height, heightErr := strconv.ParseUint(fields[8], 10, 16)
	if pidErr != nil || widthErr != nil || heightErr != nil || width == 0 || height == 0 {
		return Pane{}, fmt.Errorf("invalid tmux pane process or geometry")
	}
	if (fields[4] != "0" && fields[4] != "1") || (fields[9] != "0" && fields[9] != "1") {
		return Pane{}, fmt.Errorf("invalid tmux pane flags")
	}
	if len(fields[5]) > 256 || len(fields[6]) > 256 {
		return Pane{}, fmt.Errorf("tmux pane metadata exceeds limit")
	}
	return Pane{
		ID:        fields[2],
		RootPID:   pid,
		Dead:      fields[4] == "1",
		TTY:       fields[5],
		Command:   fields[6],
		Columns:   int(width),
		Rows:      int(height),
		Alternate: fields[9] == "1",
	}, nil
}

func parseRootPID(value string, dead bool) (int, error) {
	if value == "" && dead {
		return 0, nil
	}
	pid, err := strconv.ParseInt(value, 10, 32)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pane root PID")
	}
	return int(pid), nil
}

func appendCommand(args []string, command ...string) []string {
	if len(args) != 0 {
		args = append(args, ";")
	}
	return append(args, command...)
}

func (s *Sampler) run(ctx context.Context, limit int, args ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	args = append([]string{"-S", s.server.Socket}, args...)
	out, err := s.runner.RunLimited(ctx, limit, args...)
	if err != nil {
		return "", err
	}
	if len(out) > limit {
		return "", fmt.Errorf("tmux response exceeds %d bytes", limit)
	}
	return out, ctx.Err()
}
