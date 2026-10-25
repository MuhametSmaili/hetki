package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MuhametSmaili/hetki/internal/config"
)

func BenchmarkDecode(b *testing.B) {
	for _, fixture := range configFixtures() {
		name := fixture.name
		if name == "missing" {
			name = "empty"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := config.Decode(strings.NewReader(fixture.input)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkLoadFile(b *testing.B) {
	for _, fixture := range configFixtures() {
		b.Run(fixture.name, func(b *testing.B) {
			path := benchmarkConfigFile(b, fixture.name, fixture.input)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := config.LoadFile(b.Context(), path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func benchmarkConfigFile(b *testing.B, name, input string) string {
	b.Helper()
	b.Setenv("HOME", b.TempDir())
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	path, err := config.DefaultPath()
	if err != nil {
		b.Fatal(err)
	}
	if name == "missing" {
		return path
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(input), 0600); err != nil {
		b.Fatal(err)
	}
	return path
}

func configFixtures() []struct{ name, input string } {
	typical := "ui:\n  view: tree\n  scope: tracked\n  preview: true\n"
	return []struct{ name, input string }{
		{"missing", ""},
		{"typical", typical},
		{"max_permitted", typical + "#" + strings.Repeat("x", 65536-len(typical)-1)},
	}
}
