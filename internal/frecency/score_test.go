package frecency

import (
	"math"
	"testing"
	"time"
)

func TestScoreSevenDayHalfLifeAndFutureClock(t *testing.T) {
	now := time.Unix(20_000_000, 0)
	tests := []struct {
		name     string
		lastUsed time.Time
		want     float64
	}{
		{name: "fresh", lastUsed: now, want: 8},
		{name: "one week", lastUsed: now.Add(-7 * 24 * time.Hour), want: 4},
		{name: "two weeks", lastUsed: now.Add(-14 * 24 * time.Hour), want: 2},
		{name: "four weeks", lastUsed: now.Add(-28 * 24 * time.Hour), want: 0.5},
		{name: "future", lastUsed: now.Add(24 * time.Hour), want: 8},
		{name: "extreme future", lastUsed: time.Unix(math.MaxInt64, 0), want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Score(Record{Rank: 8, LastUsed: tt.lastUsed.Unix()}, now)

			if got != tt.want {
				t.Fatalf("Score() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScoresSumDecayPerRecord(t *testing.T) {
	now := time.Unix(2_000_000, 0)
	records := []Record{
		{Path: "/repo", Session: "recent", Rank: 2, LastUsed: now.Unix()},
		{Path: "/repo", Session: "old", Rank: 8, LastUsed: now.Add(-14 * 24 * time.Hour).Unix()},
	}

	scores := NewScores(records, now)
	if got := scores.Path("/repo"); got != 4 {
		t.Fatalf("Path() = %v, want 4", got)
	}
	if got := scores.Record("/repo", "recent"); got != 2 {
		t.Fatalf("Record(recent) = %v, want 2", got)
	}
	if got := scores.Record("/repo", "missing"); got != 0 {
		t.Fatalf("Record(missing) = %v, want 0", got)
	}
}

func TestScoresHandleDuplicateInputConsistently(t *testing.T) {
	now := time.Unix(2_000_000, 0)
	records := []Record{
		{Path: "/repo", Session: "work", Rank: 1, LastUsed: now.Unix()},
		{Path: "/repo", Session: "work", Rank: 2, LastUsed: now.Unix()},
	}
	scores := NewScores(records, now)
	if scores.Path("/repo") != 3 || scores.Record("/repo", "work") != 3 {
		t.Fatalf("duplicate scores disagree: path=%v record=%v", scores.Path("/repo"), scores.Record("/repo", "work"))
	}
}

func TestSessionRenameKeepsPathScoreWhileOrphanDecays(t *testing.T) {
	used := time.Unix(2_000_000, 0)
	records := []Record{{Path: "/repo", Session: "before", Rank: 12, LastUsed: used.Unix()}}

	before := NewScores(records, used)
	if before.Path("/repo") != 12 || before.Record("/repo", "after") != 0 {
		t.Fatalf("unexpected scores before rename: path=%v new-session=%v", before.Path("/repo"), before.Record("/repo", "after"))
	}

	after := NewScores(records, used.Add(14*24*time.Hour))
	if after.Path("/repo") != 3 {
		t.Fatalf("orphan path score = %v, want 3", after.Path("/repo"))
	}
	if after.Record("/repo", "after") != 0 {
		t.Fatalf("renamed session inherited secondary score %v", after.Record("/repo", "after"))
	}
}
