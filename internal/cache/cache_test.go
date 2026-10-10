package cache

import (
	"testing"

	"github.com/seventy-two/boxy/internal/reader"
)

func TestEnrichMoviesFromCacheOnlyPrefersOMDBPoster(t *testing.T) {
	letterboxdPoster := "https://a.ltrbxd.com/poster.jpg"
	movies := []reader.Movie{{Name: "Example Film", Year: 2001, Poster: letterboxdPoster}}
	omdbPoster := "https://omdb.example/poster.jpg"
	entries := map[string]Entry{
		"example film_2001": {Poster: omdbPoster},
	}

	EnrichMoviesFromCacheOnly(movies, entries)

	if movies[0].Poster != omdbPoster {
		t.Fatalf("primary poster = %q, want OMDB poster %q", movies[0].Poster, omdbPoster)
	}
	if movies[0].FallbackPoster != letterboxdPoster {
		t.Fatalf("fallback poster = %q, want Letterboxd poster %q", movies[0].FallbackPoster, letterboxdPoster)
	}
}

func TestEnrichMoviesFromCacheOnlySkipsKnownIncorrectMatches(t *testing.T) {
	movies := []reader.Movie{
		{Name: "Obsession", Year: 2025, Poster: "https://a.ltrbxd.com/obsession.jpg"},
		{Name: "Backrooms", Year: 2026, Poster: "https://a.ltrbxd.com/backrooms.jpg"},
	}
	entries := map[string]Entry{
		"obsession_2025": {Poster: "https://omdb.example/obsession.jpg", Director: "Wrong Director"},
		"backrooms_2026": {Poster: "https://omdb.example/backrooms.jpg", Director: "Wrong Director"},
	}

	EnrichMoviesFromCacheOnly(movies, entries)

	for _, movie := range movies {
		if movie.Poster != "https://a.ltrbxd.com/"+NormalizeTitle(movie.Name)+".jpg" {
			t.Errorf("%s primary poster was changed: %q", movie.Name, movie.Poster)
		}
		if movie.Director != "" || movie.FallbackPoster != "" {
			t.Errorf("%s received known-incorrect OMDB metadata: %+v", movie.Name, movie)
		}
	}
}
