package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/MuhametSmaili/hetki/internal/status"
	"github.com/stretchr/testify/require"
)

func BenchmarkSnapshotTransport(b *testing.B) {
	for _, count := range []int{0, 40, status.MaxPanes} {
		b.Run(fmt.Sprintf("panes=%d", count), func(b *testing.B) {
			c := fixtureClient(b, "benchmark")
			values := make(map[status.PaneRef]status.Value, count)
			deadline := time.Now().Add(time.Hour)
			for i := range count {
				pane := status.PaneRef{Server: c.server, ID: fmt.Sprintf("%%%d", i), Generation: "birth"}
				values[pane] = status.Value{State: status.Working, ValidUntil: deadline}
			}

			snapshot, err := status.NewSnapshot(values)
			require.NoError(b, err)
			startFixture(b, c, liveBinding(c.server), staticSource(snapshot))
			b.ReportAllocs()

			for b.Loop() {
				if _, err := readFirst(context.Background(), c); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
