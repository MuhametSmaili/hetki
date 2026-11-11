package cmd

import (
	"context"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/app/actions"
	apptui "github.com/MuhametSmaili/hetki/internal/app/tui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionCommandValidatesBeforeOpeningPicker(t *testing.T) {
	oldOpen, oldTerminal := openActions, isTerminal
	t.Cleanup(func() { openActions, isTerminal = oldOpen, oldTerminal })
	isTerminal = func(uintptr) bool { return true }
	var subjects []actions.Subject
	openActions = func(_ context.Context, subject actions.Subject, _ string, _ apptui.Overrides) error {
		subjects = append(subjects, subject)
		return nil
	}
	for _, test := range []struct {
		name  string
		args  []string
		valid bool
	}{
		{"pane", []string{"--pane", "%42"}, true},
		{"session", []string{"--session", "$3"}, true},
		{"window", []string{"--window", "@8"}, true},
		{"infer subject", nil, true},
		{"multiple subjects", []string{"--pane", "%42", "--session", "$3"}, false},
		{"empty pane", []string{"--pane", ""}, false},
		{"empty window", []string{"--window", ""}, false},
		{"empty session", []string{"--session", ""}, false},
		{"old pane flag", []string{"--pane-id", "%42"}, false},
		{"session name", []string{"--session", "work"}, false},
		{"bad ID", []string{"--pane", "%42;kill-server"}, false},
		{"bad height", []string{"--pane", "%42", "--height", "0"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := len(subjects)
			cmd := newActionCommand()
			cmd.SilenceErrors, cmd.SilenceUsage = true, true
			cmd.SetArgs(test.args)

			err := cmd.ExecuteContext(t.Context())

			if test.valid {
				require.NoError(t, err)
				assert.Len(t, subjects, before+1)
				if test.args == nil {
					assert.Equal(t, actions.Subject{}, subjects[before])
				}
			} else {
				require.Error(t, err)
				assert.Len(t, subjects, before)
			}
		})
	}
}
