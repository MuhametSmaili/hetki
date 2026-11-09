package list

import (
	"fmt"
	"testing"
)

// Each operation applies a query and then clears it, not just one keystroke.
func BenchmarkFilterRoundtrip(b *testing.B) {
	for _, size := range []int{100, 1_000, 10_000} {
		for _, shape := range []string{"flat", "tree"} {
			snapshot := filterBenchmarkSnapshot(size, shape)
			for _, query := range []struct {
				name, text string
			}{
				{name: "broad", text: "project"},
				{name: "narrow", text: "project-00042"},
				{name: "no-match", text: "does-not-exist"},
			} {
				b.Run(fmt.Sprintf("%s/%d/%s", shape, size, query.name), func(b *testing.B) {
					benchmarkFilterRoundtrip(b, snapshot, query.text)
				})
			}
		}
	}
}

func benchmarkFilterRoundtrip(b *testing.B, snapshot Snapshot, query string) {
	b.Helper()
	model, err := New(snapshot)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()

	for b.Loop() {
		model.SetQuery(query)
		model.SetQuery("")
	}
}

func filterBenchmarkSnapshot(size int, shape string) Snapshot {
	items := make([]Item, size)
	for i := range items {
		id := fmt.Sprintf("project-%05d", i)
		items[i] = item(id, id+" editor", "/work/"+id)
	}
	if shape == "flat" {
		return Snapshot{Items: items}
	}

	roots := make([]Item, 0, size/10)
	for start := 0; start < size; start += 10 {
		root := items[start]
		for offset := 1; offset < 10; offset += 3 {
			branch := items[start+offset]
			branch.Children = items[start+offset+1 : start+offset+3]
			root.Children = append(root.Children, branch)
		}
		roots = append(roots, root)
	}
	return Snapshot{Items: roots}
}
