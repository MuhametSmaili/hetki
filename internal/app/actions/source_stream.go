package actions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// itemStream decodes NDJSON as stdout arrives, retaining only decoded items and
// one incomplete record. Its owner calls finish only after successful command
// completion; records from a failed or incomplete source are never published.
type itemStream struct {
	pending []byte
	items   []Item
	ids     map[string]struct{}
}

func (s *itemStream) Write(data []byte) (int, error) {
	size := len(data)
	for len(data) > 0 {
		record, rest, complete := bytes.Cut(data, []byte{'\n'})
		if len(s.pending)+len(record) >= maxRecord {
			return size - len(data), fmt.Errorf("source item %d exceeds record limit (%d bytes)", len(s.items)+1, maxRecord)
		}
		if !complete {
			s.pending = append(s.pending, record...)
			return size, nil
		}
		if len(s.pending) > 0 {
			s.pending = append(s.pending, record...)
			record = s.pending
		}
		if err := s.decodeRecord(record); err != nil {
			return size - len(data), err
		}
		s.pending = s.pending[:0]
		data = rest
	}
	return size, nil
}

func (s *itemStream) finish() ([]Item, error) {
	if len(s.pending) > 0 {
		if err := s.decodeRecord(s.pending); err != nil {
			return nil, err
		}
	}
	return s.items, nil
}

func (s *itemStream) decodeRecord(record []byte) error {
	if len(s.items) == MaxItems {
		return fmt.Errorf("source exceeds %d items", MaxItems)
	}
	var item Item
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimSuffix(record, []byte{'\r'})))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&item); err != nil {
		return fmt.Errorf("source item %d: %w", len(s.items)+1, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("source item %d has trailing data", len(s.items)+1)
	}
	if err := validateItem(item); err != nil {
		return fmt.Errorf("source item %d: %w", len(s.items)+1, err)
	}
	if _, exists := s.ids[item.ID]; exists {
		return fmt.Errorf("duplicate item ID %q", item.ID)
	}
	if s.ids == nil {
		s.ids = make(map[string]struct{})
	}
	s.ids[item.ID] = struct{}{}
	s.items = append(s.items, item)
	return nil
}
