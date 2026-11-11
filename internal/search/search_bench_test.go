package search_test

import (
	"fmt"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/search"
)

func BenchmarkPrepare(b *testing.B) {
	for _, count := range []int{100, 10_000} {
		for _, fields := range []int{1, 5} {
			b.Run(fmt.Sprintf("%d/fields%d", count, fields), func(b *testing.B) {
				entries := benchmarkEntries(count, fields)
				b.ReportAllocs()

				for b.Loop() {
					search.Prepare(entries)
				}
			})
		}
	}
}

func BenchmarkScores(b *testing.B) {
	for _, query := range []string{"project", "project-00042", "missing"} {
		b.Run(query, func(b *testing.B) {
			index := search.Prepare(benchmarkEntries(10_000, 5))
			b.ReportAllocs()

			for b.Loop() {
				index.Scores(query)
			}
		})
	}
}

func benchmarkEntries(count, fields int) [][]search.Field {
	entries := make([][]search.Field, count)
	for i := range entries {
		name := fmt.Sprintf("project-%05d", i)
		entry := []search.Field{
			{Tier: search.Primary, Text: name + " editor"},
			{Tier: search.Primary, Text: "opencode"},
			{Tier: search.Primary, Text: "node server.js"},
			{Tier: search.Secondary, Text: "/work/" + name},
			{Tier: search.Secondary, Text: "~/code/" + name},
		}
		entries[i] = entry[:fields]
	}
	return entries
}
