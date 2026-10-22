package observe

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Fixture-only parsing/native-fact bookkeeping, excluding subprocess and OS I/O.
func BenchmarkInventory(b *testing.B) {
	for _, count := range []int{40, 1024} {
		b.Run(fmt.Sprintf("panes=%d", count), func(b *testing.B) {
			var rows strings.Builder
			for n := range count {
				fmt.Fprintf(&rows, "$1|@%d|%%%d|%d|0|/dev/pts/%d|cat|80|30|0\n", n, n, 100+n, n)
			}
			out := testInventoryOutput(rows.String())
			runner := queryRunner(func(context.Context, int, ...string) (string, error) { return out, nil })
			server := Server{Socket: "/tmp/fixture.sock", PID: 42, Started: 123, Birth: "server-birth", Version: "3.8"}
			sampler, err := NewSampler(server, runner)
			if err != nil {
				b.Fatal(err)
			}
			sampler.process = fixtureProcess
			b.ReportAllocs()

			for b.Loop() {
				if _, err := sampler.Inventory(b.Context(), 1); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
