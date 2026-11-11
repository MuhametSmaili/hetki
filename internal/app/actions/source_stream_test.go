package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceStreamPreservesRecordsAcrossWrites(t *testing.T) {
	want := []Item{
		{ID: "日本", Label: "path with spaces", Value: map[string]any{"path": "line\nbreak", "number": json.Number("18446744073709551615")}},
		{ID: "second", Label: "Other", Description: "A description"},
	}
	var input strings.Builder
	for i, item := range want {
		data, err := json.Marshal(item)
		require.NoError(t, err)
		input.Write(data)
		if i == 0 {
			input.WriteString("\r\n")
		}
	}
	data := []byte(input.String())
	for _, chunkSize := range []int{1, 7, 4096} {
		t.Run(fmt.Sprintf("chunk-%d", chunkSize), func(t *testing.T) {
			var stream itemStream

			for offset := 0; offset < len(data); offset += chunkSize {
				chunk := data[offset:min(offset+chunkSize, len(data))]
				n, err := stream.Write(chunk)
				require.NoError(t, err)
				require.Equal(t, len(chunk), n)
			}
			items, err := stream.finish()

			require.NoError(t, err)
			assert.Equal(t, want, items)
		})
	}
}

func TestSourceStreamRecordLimitAcrossWrites(t *testing.T) {
	prefix := `{"id":"path","label":"`
	suffix := "\"}\n"
	for _, size := range []int{maxRecord, maxRecord + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			input := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
			var stream itemStream
			_, err := stream.Write([]byte(input[:maxRecord/2]))
			require.NoError(t, err)

			_, err = stream.Write([]byte(input[maxRecord/2:]))

			if size > maxRecord {
				require.ErrorContains(t, err, "record limit")
				return
			}
			require.NoError(t, err)
			items, err := stream.finish()
			require.NoError(t, err)
			require.Len(t, items, 1)
			assert.Len(t, items[0].Label, size-len(prefix)-len(suffix))
		})
	}
}

func TestExecutableSourceBoundsTotalBytes(t *testing.T) {
	const count = 128
	var input strings.Builder
	for i := range count {
		prefix := fmt.Sprintf("{\"id\":\"%03d\",\"label\":\"", i)
		suffix := "\"}\n"
		input.WriteString(prefix)
		input.WriteString(strings.Repeat("x", maxOutput/count-len(prefix)-len(suffix)))
		input.WriteString(suffix)
	}
	for _, test := range []struct {
		name, extra string
	}{
		{"at limit", ""},
		{"over limit", "\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "choices.jsonl")
			require.NoError(t, os.WriteFile(path, []byte(input.String()+test.extra), 0600))
			spec := SourceSpec{Exec: &Command{Argv: []string{"cat", path}}}

			items, err := (executableSource{}).List(t.Context(), spec, Request{})

			if test.extra != "" {
				require.ErrorIs(t, err, ErrOutputLimit)
				assert.Empty(t, items)
				return
			}
			require.NoError(t, err)
			assert.Len(t, items, count)
		})
	}
}

func TestSourceStreamRejectsInvalidOutputBeforeProviderExit(t *testing.T) {
	var tooMany strings.Builder
	for i := range MaxItems + 1 {
		fmt.Fprintf(&tooMany, "{\"id\":\"%d\",\"label\":\"Path\"}\n", i)
	}
	for _, test := range []struct {
		name, data, message string
	}{
		{"malformed JSON", "{broken}\n", "source item 1"},
		{"invalid item", "{\"id\":\"a\",\"label\":\" \"}\n", "nonempty ID"},
		{"duplicate ID", strings.Repeat("{\"id\":\"a\",\"label\":\"Path\"}\n", 2), "duplicate item ID"},
		{"unfinished oversized record", strings.Repeat("x", maxRecord), "record limit"},
		{"too many items", tooMany.String(), "source exceeds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, listener := streamingSourceCommand(t, test.data)
			defer listener.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				items, err := (executableSource{}).List(ctx, SourceSpec{Exec: &command}, Request{})
				assert.Empty(t, items, "failed providers must not return partial choices")
				done <- err
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("source did not stop")
				}
			})

			select {
			case err := <-done:
				require.ErrorContains(t, err, test.message)
			case <-time.After(5 * time.Second):
				t.Fatal("invalid output did not stop the running provider")
			}

			connection, err := listener.Accept()
			require.NoError(t, err)
			defer connection.Close()
			require.NoError(t, connection.SetDeadline(time.Now().Add(5*time.Second)))
			data, err := io.ReadAll(connection)
			require.NoError(t, err)
			assert.Equal(t, "ready", string(data), "provider connection must close when decoding fails")
		})
	}
}

func streamingSourceCommand(t *testing.T, data string) (Command, *net.TCPListener) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "choices.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	require.NoError(t, listener.SetDeadline(time.Now().Add(10*time.Second)))
	return helperCommand(t, "source-stream", listener.Addr().String(), path), listener
}
