package frecency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Run("absolute XDG state home", func(t *testing.T) {
		xdg := filepath.Join(t.TempDir(), "state")
		t.Setenv("XDG_STATE_HOME", xdg)
		got, err := DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(xdg, "hetki", "frecency.json")
		if got != want {
			t.Fatalf("DefaultPath() = %q, want %q", got, want)
		}
	})

	t.Run("relative XDG state home is ignored", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "relative/state")
		got, err := DefaultPath()
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(home, ".local", "state", "hetki", "frecency.json")
		if got != want {
			t.Fatalf("DefaultPath() = %q, want %q", got, want)
		}
	})

	t.Run("relative home is rejected", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", "relative/state")
		t.Setenv("HOME", "relative/home")
		if _, err := DefaultPath(); err == nil {
			t.Fatal("DefaultPath() accepted a relative home")
		}
	})
}

func TestStoreRecordReloadsMergesAgesAndWritesDeterministically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "frecency.json")
	writeStateFixture(t, path, []Record{
		{Path: "/z", Session: "old", Rank: 1, LastUsed: 1},
		{Path: "/a", Session: "work", Rank: 9999, LastUsed: 2},
	})

	store := NewStore(path)
	store.now = func() time.Time { return time.Unix(1234, 0) }
	if err := store.Record(context.Background(), "/a", "work"); err != nil {
		t.Fatal(err)
	}

	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{{Path: "/a", Session: "work", Rank: 9000, LastUsed: 1234}}
	if !recordsEqual(records, want) {
		t.Fatalf("records = %#v, want %#v", records, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version":1`) {
		t.Fatalf("state lacks version 1: %s", data)
	}
}

func TestStoreRecordKeepsOneExactRawKeyAndAddsOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	store := NewStore(path)
	store.now = func() time.Time { return time.Unix(100, 0) }

	for range 2 {
		if err := store.Record(context.Background(), "/Repo", "Work"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Record(context.Background(), "/repo", "Work"); err != nil {
		t.Fatal(err)
	}

	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []Record{
		{Path: "/Repo", Session: "Work", Rank: 2, LastUsed: 100},
		{Path: "/repo", Session: "Work", Rank: 1, LastUsed: 100},
	}
	if !recordsEqual(records, want) {
		t.Fatalf("records = %#v, want %#v", records, want)
	}
}

func TestStoreLoadStrictValidationAndBounds(t *testing.T) {
	validRecord := `{"path":"/repo","session":"work","rank":1,"last_used":1}`
	tests := []struct {
		name string
		data string
	}{
		{name: "unknown version", data: `{"version":2,"records":[]}`},
		{name: "missing version", data: `{"records":[]}`},
		{name: "missing records", data: `{"version":1}`},
		{name: "null records", data: `{"version":1,"records":null}`},
		{name: "unknown field", data: `{"version":1,"records":[],"extra":true}`},
		{name: "record unknown field", data: `{"version":1,"records":[{"path":"/repo","session":"work","rank":1,"last_used":1,"extra":true}]}`},
		{name: "missing last used", data: `{"version":1,"records":[{"path":"/repo","session":"work","rank":1}]}`},
		{name: "excessive JSON depth", data: `{"version":1,"records":[],"extra":` + strings.Repeat("[", maxJSONDepth+1) + `0` + strings.Repeat("]", maxJSONDepth+1) + `}`},
		{name: "duplicate object field", data: `{"version":2,"version":1,"records":[]}`},
		{name: "unpaired surrogate", data: `{"version":1,"records":[{"path":"\ud800","session":"work","rank":1,"last_used":1}]}`},
		{name: "trailing document", data: `{"version":1,"records":[]} {}`},
		{name: "duplicate exact key", data: `{"version":1,"records":[` + validRecord + `,` + validRecord + `]}`},
		{name: "empty path", data: `{"version":1,"records":[{"path":"","session":"work","rank":1,"last_used":1}]}`},
		{name: "empty session", data: `{"version":1,"records":[{"path":"/repo","session":"","rank":1,"last_used":1}]}`},
		{name: "zero rank", data: `{"version":1,"records":[{"path":"/repo","session":"work","rank":0,"last_used":1}]}`},
		{name: "rank below eviction floor", data: `{"version":1,"records":[{"path":"/repo","session":"work","rank":0.9,"last_used":1}]}`},
		{name: "rank above bound", data: `{"version":1,"records":[{"path":"/repo","session":"work","rank":10001,"last_used":1}]}`},
		{name: "path above bound", data: `{"version":1,"records":[{"path":"` + strings.Repeat("p", maxPathBytes+1) + `","session":"work","rank":1,"last_used":1}]}`},
		{name: "session above bound", data: `{"version":1,"records":[{"path":"/repo","session":"` + strings.Repeat("s", maxSessionBytes+1) + `","rank":1,"last_used":1}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "frecency.json")
			if err := os.WriteFile(path, []byte(tt.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(path).Load(); err == nil {
				t.Fatal("Load() succeeded, want validation error")
			}
		})
	}

	t.Run("file bytes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "frecency.json")
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(maxJSONBytes + 1); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(path).Load(); err == nil {
			t.Fatal("Load() accepted oversized JSON")
		}
	})
}

func TestStoreCorruptUnknownAndUnreadableRecovery(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "corrupt", data: []byte(`{"version":`)},
		{name: "unknown", data: []byte(`{"version":99,"records":[]}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "frecency.json")
			if err := os.WriteFile(path, tt.data, 0600); err != nil {
				t.Fatal(err)
			}
			store := NewStore(path)
			store.now = func() time.Time { return time.Unix(123, 456) }
			if _, err := store.Load(); err == nil {
				t.Fatal("Load() succeeded on invalid state")
			}
			if err := store.Record(context.Background(), "/repo", "work"); err != nil {
				t.Fatal(err)
			}

			backups, err := filepath.Glob(path + ".corrupt-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("recovery copies = %v, err=%v", backups, err)
			}
			got, err := os.ReadFile(backups[0])
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(tt.data) {
				t.Fatalf("recovery bytes = %q, want %q", got, tt.data)
			}
			records, err := store.Load()
			if err != nil || len(records) != 1 || records[0].Rank != 1 {
				t.Fatalf("clean records = %#v, err=%v", records, err)
			}
		})
	}

	t.Run("unreadable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "frecency.json")
		if err := os.WriteFile(path, []byte(`{"version":1,"records":[]}`), 0000); err != nil {
			t.Fatal(err)
		}
		store := NewStore(path)
		if _, err := store.Load(); err == nil {
			t.Skip("current user can read mode-000 files")
		}
		if err := store.Record(context.Background(), "/repo", "work"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err != nil {
			t.Fatalf("clean state remains unreadable: %v", err)
		}
	})
}

func TestStoreAtomicRenameFailurePreservesValidState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	writeStateFixture(t, path, []Record{{Path: "/old", Session: "work", Rank: 1, LastUsed: 1}})
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	store.rename = func(_, _ string) error { return errors.New("rename denied") }
	if err := store.Record(context.Background(), "/new", "work"); err == nil {
		t.Fatal("Record() succeeded despite rename failure")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("valid state changed after failed atomic rename: %q", got)
	}
	if temps, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".frecency.json.tmp-*")); len(temps) != 0 {
		t.Fatalf("temporary files remain: %v", temps)
	}
}

func TestStorePreservationFailureNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	original := []byte(`{"version":`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	store := NewStore(path)
	store.rename = func(_, _ string) error { return errors.New("preserve denied") }
	err := store.Record(context.Background(), "/repo", "work")
	if err == nil || !strings.Contains(err.Error(), "preserv") {
		t.Fatalf("Record() error = %v, want preservation error", err)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("original bytes changed: %q", got)
	}
	if backups, _ := filepath.Glob(path + ".corrupt-*"); len(backups) != 0 {
		t.Fatalf("unexpected recovery files: %v", backups)
	}
}

func TestStorePersistsRecordsInDeterministicKeyOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	writeRawStateFixture(t, path, []Record{
		{Path: "/z", Session: "b", Rank: 1, LastUsed: 1},
		{Path: "/a", Session: "z", Rank: 1, LastUsed: 1},
		{Path: "/a", Session: "a", Rank: 1, LastUsed: 1},
	})
	store := NewStore(path)
	store.now = func() time.Time { return time.Unix(2, 0) }
	if err := store.Record(context.Background(), "/z", "b"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var state diskState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	want := []Record{
		{Path: "/a", Session: "a", Rank: 1, LastUsed: 1},
		{Path: "/a", Session: "z", Rank: 1, LastUsed: 1},
		{Path: "/z", Session: "b", Rank: 2, LastUsed: 2},
	}
	if !recordsEqual(state.Records, want) {
		t.Fatalf("on-disk records = %#v, want %#v", state.Records, want)
	}
}

func TestStoreCreatesPrivateDirectoryAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "nested", "frecency.json")
	if err := NewStore(path).Record(context.Background(), "/repo", "work"); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0700 {
			t.Fatalf("directory %s mode = %04o, want 0700", dir, got)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("state mode = %04o, want 0600", got)
	}
}

func TestStoreRejectsNonRegularStateAndLockFiles(t *testing.T) {
	t.Run("state FIFO", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "frecency.json")
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(path).Load(); err == nil {
			t.Fatal("Load() accepted a FIFO")
		}
	})

	t.Run("lock symlink", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "frecency.json")
		target := filepath.Join(dir, "target")
		if err := os.WriteFile(target, []byte("target"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path+".lock"); err != nil {
			t.Fatal(err)
		}
		if err := NewStore(path).Record(context.Background(), "/repo", "work"); err == nil {
			t.Fatal("Record() accepted a symlink lock")
		}
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0644 {
			t.Fatalf("lock target mode changed to %04o", got)
		}
	})
}

func TestStoreLoadContextRejectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewStore(filepath.Join(t.TempDir(), "frecency.json")).LoadContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadContext() error = %v, want canceled", err)
	}
}

func TestStoreRecordCancellationAfterReloadDoesNotCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	writeStateFixture(t, path, []Record{{Path: "/repo", Session: "work", Rank: 1, LastUsed: 1}})
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	store := NewStore(path)
	store.now = func() time.Time {
		cancel()
		return time.Unix(2, 0)
	}
	if err := store.Record(ctx, "/repo", "work"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Record() error = %v, want canceled", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("state changed after cancellation: %q", got)
	}
}

func TestStoreRecordCancelsWhileWaitingForProcessLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frecency.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
			t.Error(err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = NewStore(path).Record(ctx, "/repo", "work")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Record() error = %v, want deadline exceeded", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state file exists after canceled lock: %v", err)
	}
}

func TestStoreConcurrentSubprocessWriters(t *testing.T) {
	if os.Getenv("HETKI_FRECENCY_WRITER") != "" {
		t.Skip("writer helper is run by TestStoreWriterProcess")
	}
	path := filepath.Join(t.TempDir(), "frecency.json")
	const processes = 4
	const writes = 12

	commands := make([]*exec.Cmd, processes)
	outputs := make([]bytes.Buffer, processes)
	for i := range commands {
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestStoreWriterProcess$")
		commands[i].Env = append(os.Environ(),
			"HETKI_FRECENCY_WRITER=1",
			"HETKI_FRECENCY_PATH="+path,
			"HETKI_FRECENCY_WRITES="+strconv.Itoa(writes),
		)
		commands[i].Stdout = &outputs[i]
		commands[i].Stderr = &outputs[i]
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("writer failed: %v\n%s", err, outputs[i].Bytes())
		}
	}

	records, err := NewStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Rank != processes*writes {
		t.Fatalf("records = %#v, want rank %d", records, processes*writes)
	}
}

func TestStoreWriterProcess(t *testing.T) {
	if os.Getenv("HETKI_FRECENCY_WRITER") == "" {
		return
	}
	writes, err := strconv.Atoi(os.Getenv("HETKI_FRECENCY_WRITES"))
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(os.Getenv("HETKI_FRECENCY_PATH"))
	for range writes {
		if err := store.Record(context.Background(), "/repo", "work"); err != nil {
			t.Fatal(err)
		}
	}
}

func recordsEqual(got, want []Record) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func writeStateFixture(t testing.TB, path string, records []Record) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	store := NewStore(path)
	if err := store.write(records); err != nil {
		t.Fatal(err)
	}
}

func writeRawStateFixture(t testing.TB, path string, records []Record) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(struct {
		Version int      `json:"version"`
		Records []Record `json:"records"`
	}{Version: Version, Records: records})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
