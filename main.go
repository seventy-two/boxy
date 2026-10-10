package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/seventy-two/boxy/internal/cache"
	"github.com/seventy-two/boxy/internal/letterboxd"
	"github.com/seventy-two/boxy/internal/omdb"
	"github.com/seventy-two/boxy/internal/reader"
	"github.com/seventy-two/boxy/internal/web"
)

type movieStore struct {
	mu     sync.RWMutex
	movies []reader.Movie
}

func (s *movieStore) Snapshot() []reader.Movie {
	s.mu.RLock()
	defer s.mu.RUnlock()

	movies := make([]reader.Movie, len(s.movies))
	copy(movies, s.movies)
	return movies
}

func (s *movieStore) Replace(movies []reader.Movie) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.movies = movies
}

func main() {
	omdbAPIKey := os.Getenv("MOVIE_API_KEY")
	port := os.Getenv("PORT")
	letterboxdUsername := os.Getenv("LETTERBOXD_USERNAME")
	if letterboxdUsername == "" {
		letterboxdUsername = "dongdominator"
	}

	if port == "" {
		port = "6000"
	}

	var (
		err        error
		movies     []reader.Movie
		omdbClient *omdb.Client
	)

	// Load OMDB cache
	omdbCache := cache.LoadOMDBCache()

	if omdbAPIKey != "" {
		log.Println("OMDB API key found, creating OMDB client and enriching movies...")
		// Create persistent OMDB client
		omdbClient = omdb.NewClient("http://www.omdbapi.com/", omdbAPIKey)
		// First read diary with reviews
		movies, err = reader.ReadMovieDiaryWithReviews("diary.csv", "reviews.csv")
		if err == nil {
			cache.EnrichMoviesWithCache(movies, omdbClient, omdbCache)
			// Save updated cache
			cache.SaveOMDBCache(omdbCache)
		}
	} else {
		log.Println("No OMDB API key found (set OMDB_API_KEY environment variable), reading basic movie data...")
		// Read diary with reviews (no OMDB enrichment)
		movies, err = reader.ReadMovieDiaryWithReviews("diary.csv", "reviews.csv")
		if err == nil {
			// Use cache-only enrichment
			cache.EnrichMoviesFromCacheOnly(movies, omdbCache)
		}
	}

	if err != nil {
		log.Fatal(err)
	}

	// Sort movies by watched date (most recent first)
	sort.Slice(movies, func(i, j int) bool {
		return movies[i].WatchedDate.After(movies[j].WatchedDate)
	})

	store := &movieStore{movies: movies}
	log.Printf("Loaded %d movies from local CSV fallback", len(movies))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go refreshMovies(ctx, store, letterboxd.NewScraper(letterboxdUsername), omdbClient, omdbCache)

	http.HandleFunc("/", web.HomeHandler(store.Snapshot, omdbClient != nil))
	http.HandleFunc("/api/movies", web.ApiHandler(store.Snapshot, omdbClient != nil))

	log.Printf("Starting server on port %s...", port)
	log.Printf("Visit http://localhost:%s to view your movie diary", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func refreshMovies(ctx context.Context, store *movieStore, scraper *letterboxd.Scraper, omdbClient *omdb.Client, omdbCache map[string]cache.Entry) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		latestMovies, err := scraper.FetchLatestEntries(refreshCtx)
		if err != nil {
			log.Printf("Failed to refresh Letterboxd reviews; retaining current data: %v", err)
		} else {
			movies := mergeMovieSnapshots(store.Snapshot(), latestMovies)
			if omdbClient != nil {
				cache.EnrichMoviesWithCache(movies, omdbClient, omdbCache)
				cache.SaveOMDBCache(omdbCache)
			} else {
				cache.EnrichMoviesFromCacheOnly(movies, omdbCache)
			}
			if err := reader.WriteMovieDiaryAndReviews("diary.csv", "reviews.csv", movies); err != nil {
				log.Printf("Failed to update CSV fallbacks: %v", err)
			} else {
				log.Printf("Updated CSV fallbacks with %d diary entries", len(movies))
			}

			sort.Slice(movies, func(i, j int) bool {
				return movies[i].WatchedDate.After(movies[j].WatchedDate)
			})
			store.Replace(movies)
			log.Printf("Refreshed %d reviews from Letterboxd", len(movies))
		}
		cancel()

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func mergeMovieSnapshots(cached, latest []reader.Movie) []reader.Movie {
	merged := append([]reader.Movie(nil), cached...)
	indexByKey := make(map[string]int, len(merged)+len(latest))
	for index, movie := range merged {
		indexByKey[movieSnapshotKey(movie)] = index
	}

	for _, movie := range latest {
		key := movieSnapshotKey(movie)
		index, exists := indexByKey[key]
		if !exists {
			indexByKey[key] = len(merged)
			merged = append(merged, movie)
			continue
		}

		cachedMovie := merged[index]
		if movie.Review == "" {
			movie.Review = cachedMovie.Review
		}
		if movie.Tags == "" {
			movie.Tags = cachedMovie.Tags
		}
		if movie.Poster == "" {
			movie.Poster = cachedMovie.Poster
		}
		if movie.FallbackPoster == "" {
			movie.FallbackPoster = cachedMovie.FallbackPoster
		}
		if movie.Director == "" {
			movie.Director = cachedMovie.Director
		}
		if movie.Actors == "" {
			movie.Actors = cachedMovie.Actors
		}
		if movie.Plot == "" {
			movie.Plot = cachedMovie.Plot
		}
		if movie.Genre == "" {
			movie.Genre = cachedMovie.Genre
		}
		if movie.ImdbRating == "" {
			movie.ImdbRating = cachedMovie.ImdbRating
		}
		if movie.Metascore == "" {
			movie.Metascore = cachedMovie.Metascore
		}
		if movie.RottenTomatoes == "" {
			movie.RottenTomatoes = cachedMovie.RottenTomatoes
		}
		if movie.ImdbID == "" {
			movie.ImdbID = cachedMovie.ImdbID
		}
		merged[index] = movie
	}
	return merged
}

func movieSnapshotKey(movie reader.Movie) string {
	date := movie.WatchedDate
	if date.IsZero() {
		date = movie.Date
	}
	return strings.ToLower(strings.TrimSpace(movie.Name)) + "|" + strconv.Itoa(movie.Year) + "|" + date.Format("2006-01-02")
}
