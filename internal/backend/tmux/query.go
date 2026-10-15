package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/MuhametSmaili/hetki/internal/backend"
	"github.com/MuhametSmaili/hetki/internal/backend/tmux/internal/format"
)

type Query[T any] interface {
	Args() []string
	Parse(output string) (T, error)
}

type queryParseError struct{ err error }

func (e *queryParseError) Error() string { return e.err.Error() }
func (e *queryParseError) Unwrap() error { return e.err }

func RunQuery[T any](ctx context.Context, c Client, q Query[T]) (T, error) {
	output, err := c.Run(ctx, q.Args()...)
	result, parseErr := q.Parse(output)
	if parseErr != nil {
		var zero T
		return zero, errors.Join(err, &queryParseError{err: parseErr})
	}
	return result, err
}

type Session struct {
	ID            string
	Name          string
	WorkspacePath string
	DefaultShell  string
	Windows       []Window
}

type LoadStateResult struct {
	Sessions        []Session
	Active          ActiveContext
	PaneBaseIndex   int
	WindowBaseIndex int
	clients         []clientState
}

type ActiveContext struct {
	SessionID   string
	Session     string
	WindowID    string
	Window      string
	WindowIndex int
	PaneID      string
	Pane        int
	Path        string
}

type LoadStateQuery struct {
	IncludeClients     bool
	IncludeProgramInfo bool
}

func (q LoadStateQuery) Args() []string {
	args := []string{
		"start-server",
		";", "show-options", "-gv", "base-index",
		";", "show-options", "-gv", "pane-base-index",
		";", "list-panes", "-a",
		"-F", "#{session_id}|#{q:session_name}|#{window_id}|#{q:window_name}|#{window_index}|#{window_layout}|#{window_zoomed_flag}|#{window_active}|#{pane_id}|#{pane_index}|#{pane_active}|#{q:pane_current_path}|#{q:pane_current_command}|#{q:" + backend.WorkspacePathOption + "}",
	}
	if q.IncludeProgramInfo {
		args[len(args)-1] += "|#{pane_pid}|#{pane_dead}|#{q:default-shell}"
	}
	if q.IncludeClients {
		args = append(args, ";", "list-clients", "-F", "client|#{session_id}|#{pane_id}|#{q:client_last_session}")
	}
	return args
}

func (q LoadStateQuery) Parse(output string) (LoadStateResult, error) {
	currentID := getCurrentSessionID()
	builder := newStateBuilder()

	lines := strings.Split(output, "\n")
	// The single trailing blank line is the record terminator; any other
	// blank line means a value contained a newline.
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if len(lines) < 2 {
		return LoadStateResult{}, fmt.Errorf("tmux state output is missing base indexes")
	}
	windowBaseIndex, err := parseIndex("window base", lines[0])
	if err != nil {
		return LoadStateResult{}, err
	}
	paneBaseIndex, err := parseIndex("pane base", lines[1])
	if err != nil {
		return LoadStateResult{}, err
	}
	result := LoadStateResult{WindowBaseIndex: windowBaseIndex, PaneBaseIndex: paneBaseIndex}

	for i, line := range lines[2:] {
		if line == "" {
			return LoadStateResult{}, fmt.Errorf("invalid tmux pane row %d: blank line means a value contained a newline", i+1)
		}
		if q.IncludeClients && strings.HasPrefix(line, "client|") {
			client, err := parseClientLine(line)
			if err != nil {
				return LoadStateResult{}, fmt.Errorf("invalid tmux client row: %w", err)
			}
			result.clients = append(result.clients, client)
			continue
		}
		p, err := parsePaneLineWithProgramInfo(line, q.IncludeProgramInfo)
		if err != nil {
			return LoadStateResult{}, fmt.Errorf("invalid tmux pane row %d: %w", i+1, err)
		}
		builder.addPane(p, currentID)
	}

	built := builder.result()
	result.Sessions = built.Sessions
	result.Active = built.Active

	return result, nil
}

type paneLine struct {
	sessionID, sessionName, windowID, windowName string
	windowIndex                                  int
	windowLayout                                 string
	windowZoomed                                 bool
	windowActive                                 bool
	paneID                                       string
	paneIndex                                    int
	paneActive                                   bool
	panePath, paneCmd                            string
	workspacePath                                string
	defaultShell                                 string
	panePID                                      int
	paneDead                                     bool
}

const paneFieldCount = 14

func parsePaneLine(line string) (paneLine, error) {
	return parsePaneLineWithProgramInfo(line, false)
}

func parsePaneLineWithProgramInfo(line string, programInfo bool) (paneLine, error) {
	count := paneFieldCount
	if programInfo {
		count += 3
	}
	fields, err := format.Fields(line, count)
	if err != nil {
		return paneLine{}, err
	}

	p := paneLine{
		sessionID:     fields[0],
		sessionName:   fields[1],
		windowID:      fields[2],
		windowName:    fields[3],
		windowLayout:  fields[5],
		paneID:        fields[8],
		panePath:      fields[11],
		paneCmd:       fields[12],
		workspacePath: fields[13],
	}
	if p.windowIndex, err = parseIndex("window", fields[4]); err != nil {
		return paneLine{}, err
	}
	if p.windowZoomed, err = parseFlag("window zoom", fields[6]); err != nil {
		return paneLine{}, err
	}
	if p.windowActive, err = parseFlag("window active", fields[7]); err != nil {
		return paneLine{}, err
	}
	if p.paneIndex, err = parseIndex("pane", fields[9]); err != nil {
		return paneLine{}, err
	}
	if p.paneActive, err = parseFlag("pane active", fields[10]); err != nil {
		return paneLine{}, err
	}
	if err := format.ValidateObjectID("session", p.sessionID, '$'); err != nil {
		return paneLine{}, err
	}
	if err := format.ValidateObjectID("window", p.windowID, '@'); err != nil {
		return paneLine{}, err
	}
	if err := format.ValidateObjectID("pane", p.paneID, '%'); err != nil {
		return paneLine{}, err
	}
	if programInfo {
		p.defaultShell = fields[16]
		if p.panePID, err = parseIndex("pane PID", fields[14]); err != nil || p.panePID == 0 {
			return paneLine{}, fmt.Errorf("invalid pane PID %q", fields[14])
		}
		if p.paneDead, err = parseFlag("pane dead", fields[15]); err != nil {
			return paneLine{}, err
		}
	}
	return p, nil
}

func parseIndex(kind, value string) (int, error) {
	index, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || index < 0 {
		return 0, fmt.Errorf("invalid %s index %q", kind, value)
	}
	return index, nil
}

func parseFlag(kind, value string) (bool, error) {
	switch value {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("invalid %s flag %q", kind, value)
	}
}

type stateBuilder struct {
	sessions map[string]*Session
	active   ActiveContext
}

func newStateBuilder() *stateBuilder {
	return &stateBuilder{sessions: make(map[string]*Session)}
}

func (b *stateBuilder) addPane(p paneLine, currentID string) {
	if p.sessionID == currentID {
		b.active.SessionID = p.sessionID
		b.active.Session = p.sessionName
		if p.windowActive {
			b.active.WindowID = p.windowID
			b.active.Window = p.windowName
			b.active.WindowIndex = p.windowIndex
		}
		if p.windowActive && p.paneActive {
			b.active.PaneID = p.paneID
			b.active.Pane = p.paneIndex
			b.active.Path = p.panePath
		}
	}

	sess := b.getOrCreateSession(p.sessionID, p.sessionName)
	sess.DefaultShell = p.defaultShell
	if sess.WorkspacePath == "" && p.workspacePath != "" {
		sess.WorkspacePath = p.workspacePath
	}
	win := b.getOrCreateWindow(sess, p.windowID, p.windowName, p.windowIndex, p.windowLayout, p.panePath)
	win.Active = p.windowActive
	win.Panes = append(win.Panes, Pane{ID: p.paneID, Index: p.paneIndex, Path: p.panePath, Command: p.paneCmd, PID: p.panePID, Dead: p.paneDead, Zoom: p.windowZoomed && p.paneActive, Active: p.paneActive})
}

func (b *stateBuilder) getOrCreateSession(id, name string) *Session {
	if sess, ok := b.sessions[id]; ok {
		return sess
	}
	sess := &Session{ID: id, Name: name}
	b.sessions[id] = sess
	return sess
}

func (b *stateBuilder) getOrCreateWindow(sess *Session, id, name string, index int, layout, path string) *Window {
	for i := range sess.Windows {
		if sess.Windows[i].ID == id {
			if layout != "" {
				sess.Windows[i].Layout = layout
			}
			return &sess.Windows[i]
		}
	}
	sess.Windows = append(sess.Windows, Window{ID: id, Name: name, Index: index, Path: path, Layout: layout})
	return &sess.Windows[len(sess.Windows)-1]
}

func (b *stateBuilder) result() LoadStateResult {
	sessions := make([]Session, 0, len(b.sessions))
	for _, session := range b.sessions {
		copy := *session
		sort.Slice(copy.Windows, func(i, j int) bool {
			return copy.Windows[i].Index < copy.Windows[j].Index
		})
		sessions = append(sessions, copy)
	}
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].Name == sessions[j].Name {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].Name < sessions[j].Name
	})
	return LoadStateResult{Sessions: sessions, Active: b.active}
}

func getCurrentSessionID() string {
	tmuxEnv := os.Getenv("TMUX")
	if tmuxEnv == "" {
		return ""
	}
	parts := strings.Split(tmuxEnv, ",")
	if len(parts) < 3 {
		return ""
	}
	return "$" + parts[2]
}

type clientState struct {
	sessionID   string
	paneID      string
	lastSession string
}

func parseClientLine(line string) (clientState, error) {
	fields, err := format.Fields(line, 4)
	if err != nil {
		return clientState{}, err
	}
	if err := format.ValidateObjectID("client session", fields[1], '$'); err != nil {
		return clientState{}, err
	}
	if err := format.ValidateObjectID("client pane", fields[2], '%'); err != nil {
		return clientState{}, err
	}
	return clientState{sessionID: fields[1], paneID: fields[2], lastSession: fields[3]}, nil
}

func invokingClient(clients []clientState) clientState {
	if !isInsideTmux() {
		return clientState{}
	}
	pane, session := os.Getenv("TMUX_PANE"), getCurrentSessionID()
	// Popup jobs have TMUX's session ID but usually no TMUX_PANE.
	if (pane != "" && format.ValidateObjectID("invoking pane", pane, '%') != nil) ||
		(pane == "" && format.ValidateObjectID("invoking session", session, '$') != nil) {
		return clientState{}
	}
	var found clientState
	for _, client := range clients {
		if (pane != "" && client.paneID != pane) || (pane == "" && client.sessionID != session) {
			continue
		}
		if found.sessionID != "" {
			// ponytail: shared contexts are ambiguous; add explicit popup
			// client identity if that use case needs an indicator.
			return clientState{}
		}
		found = client
	}
	return found
}
