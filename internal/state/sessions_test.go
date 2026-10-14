package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareSessions(t *testing.T) {
	tests := []struct {
		name    string
		desired *State
		actual  *State
		test    func(t *testing.T, diff Diff)
	}{
		{
			name: "Missing session when not in actual",
			desired: &State{Sessions: map[string]*Session{
				"session1": {Name: "session1"},
			}},
			actual: NewState(),
			test: func(t *testing.T, diff Diff) {
				assert.Len(t, diff.Sessions.Missing, 1)
				assert.Contains(t, diff.Sessions.Missing, "session1")
			},
		},
		{
			name:    "Extra session when not in desired",
			desired: NewState(),
			actual: &State{Sessions: map[string]*Session{
				"session1": {Name: "session1"},
			}},
			test: func(t *testing.T, diff Diff) {
				assert.Len(t, diff.Sessions.Extra, 1)
				assert.Contains(t, diff.Sessions.Extra, "session1")
			},
		},
		{
			name: "No diff when sessions match",
			desired: &State{Sessions: map[string]*Session{
				"session1": {Name: "session1"},
			}},
			actual: &State{Sessions: map[string]*Session{
				"session1": {Name: "session1"},
			}},
			test: func(t *testing.T, diff Diff) {
				assert.Empty(t, diff.Sessions.Missing)
				assert.Empty(t, diff.Sessions.Extra)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := Compare(tt.desired, tt.actual, nil)
			tt.test(t, diff)
		})
	}
}

func TestCompareSessionsOrdersResultsBySessionName(t *testing.T) {
	desired := &State{Sessions: map[string]*Session{
		"gamma": {Name: "gamma"},
		"alpha": {Name: "alpha"},
		"beta":  {Name: "beta"},
	}}
	actual := &State{Sessions: map[string]*Session{
		"delta": {Name: "delta"},
		"beta":  {Name: "beta"},
		"omega": {Name: "omega"},
	}}

	for range 100 {
		diff := Compare(desired, actual, nil)
		assert.Equal(t, []string{"alpha", "gamma"}, diff.Sessions.Missing)
		assert.Equal(t, []string{"delta", "omega"}, diff.Sessions.Extra)
		assert.Equal(t, []string{"beta"}, CommonSessions(desired, actual))
	}
}
