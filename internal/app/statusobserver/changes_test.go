package statusobserver

import (
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func TestChangesSignalsNewEvidenceButNotRepeatedReports(t *testing.T) {
	source, row := sourceFixture(t)
	scope := sourceScope(row.facts)
	source.Reset(scope)
	require.NoError(t, source.Output(scope, []byte("\x1b]7501;state=idle\a")))
	changes := source.Changes()

	require.NoError(t, source.Output(scope, []byte("\x1b]7501;state=idle\a")))
	select {
	case <-changes:
		t.Fatal("a repeated report signaled a change")
	default:
	}
	require.NoError(t, source.Output(scope, []byte("\x1b]7501;state=blocked\a")))
	view, err := source.ReadView(t.Context())

	require.NoError(t, err)
	select {
	case <-changes:
	default:
		t.Fatal("new evidence did not signal a change")
	}
	require.Equal(t, status.Blocked, view.Values.Leased(view.Lease).At(row.pane.Ref, time.Now()))
}
