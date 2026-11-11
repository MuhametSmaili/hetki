package list

import (
	"fmt"
	"reflect"
	"testing"
)

func BenchmarkVisibleRows(b *testing.B) {
	for _, size := range []int{100, 10_000} {
		for _, shape := range []string{"flat", "tree"} {
			b.Run(fmt.Sprintf("%s/%d", shape, size), func(b *testing.B) {
				model := benchmarkViewportModel(b, size, shape)
				b.ReportMetric(float64(reflect.TypeOf(model.rows).Elem().Size()), "stored-B/row")
				b.ReportAllocs()

				for b.Loop() {
					model.VisibleRows()
				}
			})
		}
	}
}

func BenchmarkFilterAndViewport(b *testing.B) {
	for _, shape := range []string{"flat", "tree"} {
		b.Run(shape, func(b *testing.B) {
			model := benchmarkViewportModel(b, 10_000, shape)
			b.ReportAllocs()

			for b.Loop() {
				model.SetQuery("project")
				model.VisibleRows()
				model.SetQuery("")
				model.VisibleRows()
			}
		})
	}
}

func BenchmarkFilterWithDetails(b *testing.B) {
	for _, query := range []string{"project", "project-00042", "missing"} {
		b.Run(query, func(b *testing.B) {
			model := benchmarkViewportModel(b, 10_000, "flat")
			first := model.Snapshot().Items[0]
			model.SetDetails(map[ItemID]Details{first.ID: {Secondary: "/updated", SearchFields: first.SearchFields}})
			model.SetQuery(query)
			model.SetQuery("")
			b.ReportAllocs()

			for b.Loop() {
				model.SetQuery(query)
				model.VisibleRows()
				model.SetQuery("")
				model.VisibleRows()
			}
		})
	}
}

func BenchmarkFilterWithScope(b *testing.B) {
	for _, every := range []int{1, 100} {
		b.Run(fmt.Sprintf("every%d", every), func(b *testing.B) {
			model := benchmarkViewportModel(b, 10_000, "flat")
			included := make(map[ItemID]struct{})
			for i, item := range model.Snapshot().Items {
				if i%every == 0 {
					included[item.ID] = struct{}{}
				}
			}
			model.SetFilter("project", included)
			model.SetFilter("", included)
			b.ReportAllocs()

			for b.Loop() {
				model.SetFilter("project", included)
				model.VisibleRows()
				model.SetFilter("", included)
				model.VisibleRows()
			}
		})
	}
}

func benchmarkViewportModel(b *testing.B, size int, shape string) Model {
	b.Helper()
	model, err := New(filterBenchmarkSnapshot(size, shape))
	if err != nil {
		b.Fatal(err)
	}
	model.ResizeRows(20, treeRowHeight)
	model.SetQuery("project")
	model.SetQuery("")
	return model
}
