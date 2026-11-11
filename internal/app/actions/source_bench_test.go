package actions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkExecutableSource(b *testing.B) {
	for _, size := range []int{100, 1_000, 10_000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			command, bytes := benchmarkSourceFile(b, size)
			spec := SourceSpec{Exec: &command}
			b.SetBytes(bytes)
			b.ReportAllocs()

			for b.Loop() {
				items, err := (executableSource{}).List(b.Context(), spec, Request{Version: 1})
				if err != nil {
					b.Fatal(err)
				}
				if len(items) != size {
					b.Fatalf("got %d items, want %d", len(items), size)
				}
			}
		})
	}
}

func benchmarkSourceFile(b *testing.B, size int) (Command, int64) {
	b.Helper()
	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	for i := range size {
		path := fmt.Sprintf("projects/project-%05d/internal/application/feature/implementation.go", i)
		item := Item{ID: path, Label: path, Description: "Go source file", Value: map[string]any{"path": "/work/" + path}}
		if err := encoder.Encode(item); err != nil {
			b.Fatal(err)
		}
	}
	path := filepath.Join(b.TempDir(), "choices.jsonl")
	if err := os.WriteFile(path, input.Bytes(), 0600); err != nil {
		b.Fatal(err)
	}
	return Command{Argv: []string{"cat", path}}, int64(input.Len())
}
