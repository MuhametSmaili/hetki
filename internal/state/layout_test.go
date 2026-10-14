package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareUsesOpaqueLayoutPolicyWithoutRelaxingPanes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		desired     string
		actual      string
		policyMatch bool
		zoom        bool
		wantMatch   bool
		wantPolicy  bool
	}{
		{"backend recognizes layouts", "request", "observation", true, false, true, true},
		{"backend rejects layouts", "request", "observation", false, false, false, true},
		{"zoom still matters", "request", "observation", true, true, false, true},
		{"equal observations bypass policy", "same", "same", false, false, true, false},
		{"missing request bypasses policy", "", "observation", false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			desired := NewState()
			desired.AddSession("dev").Windows = []*Window{{Name: "editor", Path: "/repo", Layout: tc.desired, Panes: []*Pane{{Path: "/repo"}}}}
			actual := NewState()
			actual.AddSession("dev").Windows = []*Window{{Name: "editor", Path: "/repo", Layout: tc.actual, Panes: []*Pane{{Path: "/repo", Zoom: tc.zoom}}}}
			called := false
			policy := func(request, observation string) bool {
				called = true
				assert.Equal(t, tc.desired, request)
				assert.Equal(t, tc.actual, observation)
				return tc.policyMatch
			}

			diff := Compare(desired, actual, policy)

			assert.Equal(t, tc.wantMatch, diff.Windows["dev"].IsEmpty())
			assert.Equal(t, tc.wantPolicy, called)
		})
	}
}
