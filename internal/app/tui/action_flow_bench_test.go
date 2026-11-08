package tui

import (
	"fmt"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/app/actions"
	"github.com/MuhametSmaili/hetki/internal/tui/list"
)

func BenchmarkActionScreen(b *testing.B) {
	for _, size := range []int{100, 1000, 10_000} {
		screen := actions.Screen{Key: "choices", Title: "Choices", Items: make([]actions.Item, size)}
		for i := range screen.Items {
			screen.Items[i] = actions.Item{ID: fmt.Sprint(i), Label: fmt.Sprintf("project-%d editor", i), Description: "/work/project"}
		}
		b.Run(fmt.Sprintf("project/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				projectActionScreen(screen)
			}
		})
		b.Run(fmt.Sprintf("filter/%d", size), func(b *testing.B) {
			items, err := list.New(*projectActionScreen(screen).Snapshot)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				items.SetQuery("project1")
				items.SetQuery("editor")
			}
		})
	}
}
