package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/backend"
	backendtmux "github.com/MuhametSmaili/hetki/internal/backend/tmux"
	"github.com/MuhametSmaili/hetki/internal/frecency"
	ui "github.com/MuhametSmaili/hetki/internal/tui"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

// One pane and unique path per window; five flat search fields, including the
// shortened home path. Fixture construction and disk writes aren't timed.
func benchmarkWorkspace(windows int) (backend.StateResult, []frecency.Record, string) {
	state := backend.StateResult{Active: backend.ActiveContext{SessionID: "$0", WindowID: "@0", PaneID: "%0"}}
	var records []frecency.Record
	var wire strings.Builder
	wire.WriteString("0\n0\n")
	names := []string{"editor", "tests", "shell", "logs"}
	for s := range 1_000 {
		session := backend.Session{ID: fmt.Sprintf("$%d", s), Name: fmt.Sprintf("project-%04d", s)}
		for w := range windows {
			id := s*windows + w
			path := fmt.Sprintf("/home/bench/Projects/%s/%s", session.Name, names[w])
			window := backend.Window{
				ID: fmt.Sprintf("@%d", id), Name: names[w], Index: w, Path: path, Active: w == 0,
				Panes: []backend.Pane{{ID: fmt.Sprintf("%%%d", id), Path: path, Command: "zsh", Active: true}},
			}
			session.Windows = append(session.Windows, window)
			records = append(records, frecency.Record{Path: path, Session: session.Name, Rank: float64(id%3 + 1), LastUsed: 2_000_000})
			active := 0
			if window.Active {
				active = 1
			}
			fmt.Fprintf(&wire, "%s|%s|%s|%s|%d|layout|0|%d|%%%d|0|1|%s|zsh|\n",
				session.ID, session.Name, window.ID, window.Name, w, active, id, path)
		}
		state.Sessions = append(state.Sessions, session)
	}
	return state, records, wire.String()
}

func BenchmarkThousandSessions(b *testing.B) {
	for _, windows := range []int{3, 4} {
		b.Run(fmt.Sprintf("windows=%d", windows), func(b *testing.B) {
			state, records, wire := benchmarkWorkspace(windows)
			scores := frecency.NewScores(records, time.Unix(2_000_000, 0))
			b.Run("parse", func(b *testing.B) { benchmarkStateParsing(b, wire) })
			b.Run("history_load", func(b *testing.B) { benchmarkHistoryLoad(b, records) })
			for _, projection := range []ui.ViewMode{ui.ViewFlat, ui.ViewTree} {
				benchmarkProjection(b, state, scores, projection, windows)
			}
		})
	}
}

func benchmarkStateParsing(b *testing.B, wire string) {
	b.ReportAllocs()
	for b.Loop() {
		parsed, err := (backendtmux.LoadStateQuery{}).Parse(wire)
		if err != nil || len(parsed.Sessions) != 1_000 {
			b.Fatalf("invalid parsed fixture: %v", err)
		}
	}
}

func benchmarkHistoryLoad(b *testing.B, records []frecency.Record) {
	path := filepath.Join(b.TempDir(), "frecency.json")
	data, err := json.Marshal(struct {
		Version int               `json:"version"`
		Records []frecency.Record `json:"records"`
	}{frecency.Version, records})
	if err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		b.Fatal(err)
	}
	store := frecency.NewStore(path)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := store.Load(); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkProjection(b *testing.B, state backend.StateResult, scores frecency.Scores, projection ui.ViewMode, windows int) {
	name, wantRows := "flat", 1_000*windows
	if projection == ui.ViewTree {
		name, wantRows = "tree", wantRows+1_000
	}
	project := func(b *testing.B) list.Snapshot {
		var snapshot list.Snapshot
		var err error
		if projection == ui.ViewFlat {
			snapshot, _, err = projectFlatRanked(state, "/home/bench", scores)
		} else {
			snapshot, _, err = projectTree(state, "/home/bench")
		}
		if err != nil {
			b.Fatal(err)
		}
		return snapshot
	}
	b.Run(name+"_project", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			project(b)
		}
	})
	benchmarkProjectedList(b, name, project(b), wantRows)
}

func benchmarkProjectedList(b *testing.B, name string, snapshot list.Snapshot, wantRows int) {
	model, err := list.New(snapshot)
	if err != nil {
		b.Fatal(err)
	}
	if len(model.Rows()) != wantRows {
		b.Fatalf("%s has %d rows, want %d", name, len(model.Rows()), wantRows)
	}
	b.Run(name+"_list_init", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := list.New(snapshot); err != nil {
				b.Fatal(err)
			}
		}
	})
	for _, query := range []string{"project", "project-0999", "no-match"} {
		b.Run(name+"_filter/"+query, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				model.SetQuery(query)
			}
		})
	}
}
