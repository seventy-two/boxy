package main

import (
	"testing"
	"time"

	"github.com/seventy-two/boxy/internal/reader"
)

func TestMergeMovieSnapshotsPreservesHistoryAndEnrichment(t *testing.T) {
	watchedDate := time.Date(2025, time.November, 17, 0, 0, 0, 0, time.UTC)
	cached := []reader.Movie{
		{
			Name:        "Revolutionary Road",
			Year:        2008,
			WatchedDate: watchedDate,
			Rating:      4,
			Review:      "Saved review",
			Poster:      "saved-poster.jpg",
		},
		{Name: "Older Film", Year: 1999, WatchedDate: watchedDate.AddDate(-1, 0, 0)},
	}
	latest := []reader.Movie{
		{
			Name:        "Revolutionary Road",
			Year:        2008,
			WatchedDate: watchedDate,
			Rating:      4.5,
		},
		{Name: "New Film", Year: 2026, WatchedDate: watchedDate.AddDate(1, 0, 0)},
	}

	merged := mergeMovieSnapshots(cached, latest)
	if len(merged) != 3 {
		t.Fatalf("got %d movies, want 3", len(merged))
	}
	if merged[0].Rating != 4.5 || merged[0].Review != "Saved review" || merged[0].Poster != "saved-poster.jpg" {
		t.Errorf("matching entry did not update while preserving cached fields: %+v", merged[0])
	}
	if merged[1].Name != "Older Film" || merged[2].Name != "New Film" {
		t.Errorf("history or new entry missing from merged snapshot: %+v", merged)
	}
}
