package reader

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteMovieDiaryAndReviewsRoundTrip(t *testing.T) {
	directory := t.TempDir()
	diaryPath := filepath.Join(directory, "diary.csv")
	reviewsPath := filepath.Join(directory, "reviews.csv")
	watchedDate := time.Date(2026, time.October, 7, 0, 0, 0, 0, time.UTC)
	movies := []Movie{
		{
			Date:          watchedDate,
			Name:          "Rated Film",
			Year:          2024,
			LetterboxdURI: "https://letterboxd.com/film/rated-film/",
			Rating:        3.5,
			Rewatch:       true,
			Tags:          "favorites",
			WatchedDate:   watchedDate,
			Review:        "A review with, a comma and\na newline.",
		},
		{
			Date:          watchedDate,
			Name:          "Unrated Film",
			Year:          2025,
			LetterboxdURI: "https://letterboxd.com/film/unrated-film/",
			WatchedDate:   watchedDate,
		},
	}

	if err := WriteMovieDiaryAndReviews(diaryPath, reviewsPath, movies); err != nil {
		t.Fatal(err)
	}
	loaded, err := ReadMovieDiaryWithReviews(diaryPath, reviewsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("got %d movies, want 2", len(loaded))
	}
	if loaded[0].Rating != 3.5 || !loaded[0].Rewatch || loaded[0].Review != movies[0].Review {
		t.Errorf("rated movie did not round-trip: %+v", loaded[0])
	}
	if loaded[1].Rating != 0 || loaded[1].Review != "" {
		t.Errorf("unrated movie did not round-trip: %+v", loaded[1])
	}
}

func TestWriteMovieDiaryAndReviewsPreservesExistingReviewRows(t *testing.T) {
	directory := t.TempDir()
	diaryPath := filepath.Join(directory, "diary.csv")
	reviewsPath := filepath.Join(directory, "reviews.csv")
	existingReviews := "Date,Name,Year,Letterboxd URI,Rating,Rewatch,Review,Tags,Watched Date\n" +
		"2025-01-01,Film One,2000,/film-one/,4,No,Old review,,2025-01-01\n" +
		"2024-01-01,Film Two,2001,/film-two/,3,No,Older review,,2024-01-01\n"
	if err := os.WriteFile(reviewsPath, []byte(existingReviews), 0600); err != nil {
		t.Fatal(err)
	}

	movies := []Movie{
		{
			Date:          time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			Name:          "Film One",
			Year:          2000,
			LetterboxdURI: "/film-one/",
			Rating:        4.5,
			WatchedDate:   time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC),
			Review:        "Updated review",
		},
		{
			Date:          time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			Name:          "Film Three",
			Year:          2026,
			LetterboxdURI: "/film-three/",
			WatchedDate:   time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			Review:        "New review",
		},
	}
	if err := WriteMovieDiaryAndReviews(diaryPath, reviewsPath, movies); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(reviewsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 {
		t.Fatalf("got %d review records, want 4 including header", len(records))
	}
	contents := strings.Join([]string{records[1][6], records[2][6], records[3][6]}, "|")
	if contents != "Updated review|Older review|New review" {
		t.Errorf("unexpected preserved reviews: %q", contents)
	}
}
