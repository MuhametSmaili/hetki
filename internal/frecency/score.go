package frecency

import (
	"math"
	"time"
)

const halfLife = 7 * 24 * time.Hour

// Record stores history for one raw path and session name.
// Rank is the score at LastUsed, not the total number of visits.
// LastUsed is a Unix timestamp in seconds.
type Record struct {
	Path     string  `json:"path"`
	Session  string  `json:"session"`
	Rank     float64 `json:"rank"`
	LastUsed int64   `json:"last_used"`
}

type recordKey struct {
	path    string
	session string
}

// Scores contains record scores and path sums at one time.
type Scores struct {
	paths   map[string]float64
	records map[recordKey]float64
}

// Score applies continuous decay with a seven-day half-life.
// A future LastUsed does not increase the rank.
func Score(record Record, now time.Time) float64 {
	if record.LastUsed >= now.Unix() {
		return record.Rank
	}

	age := uint64(now.Unix()) - uint64(record.LastUsed)
	return record.Rank * math.Exp2(-float64(age)/halfLife.Seconds())
}

// NewScores calculates record scores and their sums for each path.
func NewScores(records []Record, now time.Time) Scores {
	scores := Scores{
		paths:   make(map[string]float64, len(records)),
		records: make(map[recordKey]float64, len(records)),
	}
	for _, record := range records {
		score := Score(record, now)
		scores.paths[record.Path] += score
		scores.records[recordKey{path: record.Path, session: record.Session}] += score
	}
	return scores
}

// Path returns the sum of the record scores for path.
func (s Scores) Path(path string) float64 {
	return s.paths[path]
}

// Record returns the score for one exact path and session pair.
func (s Scores) Record(path, session string) float64 {
	return s.records[recordKey{path: path, session: session}]
}
