package frecency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	Version         = 1
	maxJSONBytes    = 16 << 20
	maxPathBytes    = 4_096
	maxSessionBytes = 255
	maxRank         = 10_000
	maxJSONDepth    = 64
	ageThreshold    = 10_000
	lockRetry       = 10 * time.Millisecond
)

type diskState struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
}

type rawState struct {
	Version json.RawMessage `json:"version"`
	Records json.RawMessage `json:"records"`
}

type inputRecord struct {
	Path     string  `json:"path"`
	Session  string  `json:"session"`
	Rank     float64 `json:"rank"`
	LastUsed *int64  `json:"last_used"`
}

// Store persists frecency records at one state-file path.
type Store struct {
	path   string
	now    func() time.Time
	rename func(string, string) error
}

// NewStore creates a store for path.
func NewStore(path string) *Store {
	return &Store{path: path, now: time.Now, rename: os.Rename}
}

// DefaultStore creates a store at the XDG state path.
func DefaultStore() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewStore(path), nil
}

// DefaultPath returns the platform-independent XDG state location.
func DefaultPath() (string, error) {
	if stateHome := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(stateHome) {
		return filepath.Join(stateHome, "hetki", "frecency.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	if !filepath.IsAbs(home) {
		return "", errors.New("determine home directory: path is not absolute")
	}
	return filepath.Join(home, ".local", "state", "hetki", "frecency.json"), nil
}

// Load reads and validates deterministic version-1 records. A missing file is
// an empty history; every other read or validation failure is returned.
func (s *Store) Load() ([]Record, error) {
	return s.LoadContext(context.Background())
}

// LoadContext reads history with cancellation checks between bounded file and
// decode operations.
func (s *Store) LoadContext(ctx context.Context) ([]Record, error) {
	if ctx == nil {
		return nil, errors.New("load frecency state: nil context")
	}
	records, _, err := s.load(ctx)
	return records, err
}

// Record reloads history under a cancellable process lock, increments one exact
// raw path/session pair, ages if needed, and atomically replaces the state file.
func (s *Store) Record(ctx context.Context, path, session string) error {
	if ctx == nil {
		return errors.New("record frecency: nil context")
	}
	if err := validateKey(path, session); err != nil {
		return fmt.Errorf("record frecency: %w", err)
	}
	if s == nil || s.path == "" {
		return errors.New("record frecency: empty store path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create frecency directory: %w", err)
	}

	lock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}

	records, exists, loadErr := s.load(ctx)
	now := s.clock()()
	if err := ctx.Err(); err != nil {
		return err
	}
	if loadErr != nil {
		if !exists {
			return loadErr
		}
		if _, err := s.preserveInvalid(now); err != nil {
			return fmt.Errorf("preserve invalid frecency state after %w: %w", loadErr, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		records = nil
	}

	found := false
	for i := range records {
		if records[i].Path == path && records[i].Session == session {
			records[i].Rank++
			records[i].LastUsed = now.Unix()
			found = true
			break
		}
	}
	if !found {
		records = append(records, Record{Path: path, Session: session, Rank: 1, LastUsed: now.Unix()})
	}

	var total float64
	for _, record := range records {
		total += record.Rank
	}
	if total > ageThreshold {
		kept := records[:0]
		for _, record := range records {
			record.Rank *= 0.9
			if record.Rank >= 1 {
				kept = append(kept, record)
			}
		}
		records = kept
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.writeContext(ctx, records); err != nil {
		return fmt.Errorf("write frecency state: %w", err)
	}
	return nil
}

func (s *Store) load(ctx context.Context) ([]Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if s == nil || s.path == "" {
		return nil, false, errors.New("load frecency state: empty store path")
	}
	file, err := os.OpenFile(s.path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("open frecency state: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, true, fmt.Errorf("stat frecency state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, true, fmt.Errorf("frecency state is not a regular file")
	}
	if info.Size() > maxJSONBytes {
		return nil, true, fmt.Errorf("frecency state exceeds %d bytes", maxJSONBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxJSONBytes+1))
	if err != nil {
		return nil, true, fmt.Errorf("read frecency state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	if len(data) > maxJSONBytes {
		return nil, true, fmt.Errorf("frecency state exceeds %d bytes", maxJSONBytes)
	}
	if !utf8.Valid(data) {
		return nil, true, errors.New("frecency state is not valid UTF-8")
	}

	if err := validateJSONDocument(data); err != nil {
		return nil, true, fmt.Errorf("decode frecency state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	var raw rawState
	if err := decodeJSON(data, &raw); err != nil {
		return nil, true, fmt.Errorf("decode frecency state: %w", err)
	}
	if len(raw.Version) == 0 {
		return nil, true, errors.New("decode frecency state: missing version")
	}
	var version int
	if err := decodeJSON(raw.Version, &version); err != nil {
		return nil, true, fmt.Errorf("decode frecency version: %w", err)
	}
	if version != Version {
		return nil, true, fmt.Errorf("unsupported frecency version %d", version)
	}
	if len(raw.Records) == 0 {
		return nil, true, errors.New("decode frecency state: missing records")
	}
	if bytes.Equal(bytes.TrimSpace(raw.Records), []byte("null")) {
		return nil, true, errors.New("decode frecency state: records must be an array")
	}
	var input []inputRecord
	if err := decodeJSON(raw.Records, &input); err != nil {
		return nil, true, fmt.Errorf("decode frecency records: %w", err)
	}
	records := make([]Record, len(input))
	for i, record := range input {
		if record.LastUsed == nil {
			return nil, true, fmt.Errorf("decode frecency records: records[%d] is missing last_used", i)
		}
		records[i] = Record{
			Path: record.Path, Session: record.Session, Rank: record.Rank, LastUsed: *record.LastUsed,
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	if err := validateRecords(records); err != nil {
		return nil, true, fmt.Errorf("validate frecency state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	sortRecords(records)
	return records, true, nil
}

func decodeJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateJSONDocument(data []byte) error {
	if err := validateSurrogateEscapes(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, 1); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("JSON exceeds maximum depth %d", maxJSONDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return errors.New("unexpected JSON delimiter")
	}
}

func validateSurrogateEscapes(data []byte) error {
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		for i++; i < len(data) && data[i] != '"'; i++ {
			if data[i] != '\\' {
				continue
			}
			i++
			if i >= len(data) {
				return errors.New("unterminated JSON escape")
			}
			if data[i] != 'u' {
				continue
			}
			first, ok := hexQuad(data, i+1)
			if !ok {
				return errors.New("invalid JSON Unicode escape")
			}
			i += 4
			if first >= 0xD800 && first <= 0xDBFF {
				if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
					return errors.New("unpaired high surrogate in JSON string")
				}
				second, ok := hexQuad(data, i+3)
				if !ok || second < 0xDC00 || second > 0xDFFF {
					return errors.New("unpaired high surrogate in JSON string")
				}
				i += 6
			} else if first >= 0xDC00 && first <= 0xDFFF {
				return errors.New("unpaired low surrogate in JSON string")
			}
		}
	}
	return nil
}

func hexQuad(data []byte, start int) (uint16, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var value uint16
	for _, char := range data[start : start+4] {
		value <<= 4
		switch {
		case char >= '0' && char <= '9':
			value += uint16(char - '0')
		case char >= 'a' && char <= 'f':
			value += uint16(char-'a') + 10
		case char >= 'A' && char <= 'F':
			value += uint16(char-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func validateRecords(records []Record) error {
	seen := make(map[recordKey]struct{}, len(records))
	for i, record := range records {
		if err := validateKey(record.Path, record.Session); err != nil {
			return fmt.Errorf("records[%d]: %w", i, err)
		}
		if math.IsNaN(record.Rank) || math.IsInf(record.Rank, 0) || record.Rank < 1 || record.Rank > maxRank {
			return fmt.Errorf("records[%d]: rank must be finite and in [1, %d]", i, maxRank)
		}
		key := recordKey{path: record.Path, session: record.Session}
		if _, ok := seen[key]; ok {
			return fmt.Errorf("records[%d]: duplicate path/session pair", i)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateKey(path, session string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	if !utf8.ValidString(path) {
		return errors.New("path is not valid UTF-8")
	}
	if len(path) > maxPathBytes {
		return fmt.Errorf("path exceeds %d bytes", maxPathBytes)
	}
	if session == "" {
		return errors.New("session is empty")
	}
	if !utf8.ValidString(session) {
		return errors.New("session is not valid UTF-8")
	}
	if len(session) > maxSessionBytes {
		return fmt.Errorf("session exceeds %d bytes", maxSessionBytes)
	}
	return nil
}

func sortRecords(records []Record) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Path != records[j].Path {
			return records[i].Path < records[j].Path
		}
		return records[i].Session < records[j].Session
	})
}

func (s *Store) lock(ctx context.Context) (*os.File, error) {
	lock, err := os.OpenFile(s.path+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open frecency lock: %w", err)
	}
	info, err := lock.Stat()
	if err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("stat frecency lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = lock.Close()
		return nil, errors.New("frecency lock is not a regular file")
	}
	if err := lock.Chmod(0600); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("secure frecency lock: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			_ = lock.Close()
			return nil, err
		}
		err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = lock.Close()
			return nil, fmt.Errorf("lock frecency state: %w", err)
		}
		timer := time.NewTimer(lockRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = lock.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Store) preserveInvalid(now time.Time) (string, error) {
	base := s.path + ".corrupt-" + now.UTC().Format("20060102T150405.000000000Z")
	for i := range 100 {
		candidate := base
		if i > 0 {
			candidate += fmt.Sprintf("-%d", i)
		}
		_, err := os.Lstat(candidate)
		if err == nil {
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("check recovery path: %w", err)
		}
		if err := s.renameFile()(s.path, candidate); err != nil {
			return "", fmt.Errorf("rename to %s: %w", filepath.Base(candidate), err)
		}
		return candidate, nil
	}
	return "", errors.New("no unused timestamped recovery path")
}

func (s *Store) write(records []Record) error {
	return s.writeContext(context.Background(), records)
}

func (s *Store) writeContext(ctx context.Context, records []Record) error {
	if s == nil || s.path == "" {
		return errors.New("empty store path")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	records = append([]Record(nil), records...)
	if err := validateRecords(records); err != nil {
		return err
	}
	sortRecords(records)
	if records == nil {
		records = []Record{}
	}
	data, err := json.Marshal(diskState{Version: Version, Records: records})
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	data = append(data, '\n')
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(data) > maxJSONBytes {
		return fmt.Errorf("encoded state exceeds %d bytes", maxJSONBytes)
	}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".frecency.json.tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure temporary file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := s.renameFile()(tempPath, s.path); err != nil {
		return fmt.Errorf("rename temporary file: %w", err)
	}
	removeTemp = false

	directory, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open state directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync state directory: %w", err)
	}
	return nil
}

func (s *Store) clock() func() time.Time {
	if s != nil && s.now != nil {
		return s.now
	}
	return time.Now
}

func (s *Store) renameFile() func(string, string) error {
	if s != nil && s.rename != nil {
		return s.rename
	}
	return os.Rename
}
