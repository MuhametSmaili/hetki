package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
)

func benchmarkLoadResolve(b *testing.B) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		path, err := config.DefaultPath()
		if err != nil {
			b.Fatal(err)
		}
		preferences, err := config.LoadFile(b.Context(), path)
		if err != nil {
			b.Fatal(err)
		}
		if _, warning := resolveConfig(preferences); warning != "" {
			b.Fatal(warning)
		}
	}
}

// Every timed case resolves defaults (including the keymap) in the same place.
// The resolve-only baseline separates this cost from discovery and file parsing.
func BenchmarkConfigLoadResolve(b *testing.B) {
	b.Run("resolve_defaults", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, warning := resolveConfig(config.Config{}); warning != "" {
				b.Fatal(warning)
			}
		}
	})
	typical := "ui: {view: tree, scope: tracked, preview: true, preview_width: 45}\n"
	for _, fixture := range []struct{ name, input string }{
		{"missing", ""},
		{"typical", typical},
		{"max_permitted", typical + "#" + strings.Repeat("x", 65536-len(typical)-1)},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			path := privateConfig(b, fixture.input)
			if fixture.name == "missing" {
				if err := os.Remove(path); err != nil {
					b.Fatal(err)
				}
			}
			benchmarkLoadResolve(b)
		})
	}
}
