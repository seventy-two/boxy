package cache

import (
	"encoding/csv"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/seventy-two/boxy/internal/omdb"
	"github.com/seventy-two/boxy/internal/reader"
)

// Entry represents a cached OMDB entry
type Entry struct {
	Title          string `csv:"Title"`
	Year           string `csv:"Year"`
	Poster         string `csv:"Poster"`
	Director       string `csv:"Director"`
	Actors         string `csv:"Actors"`
	Plot           string `csv:"Plot"`
	Genre          string `csv:"Genre"`
	ImdbRating     string `csv:"ImdbRating"`
	Metascore      string `csv:"Metascore"`
	RottenTomatoes string `csv:"RottenTomatoes"`
	ImdbID         string `csv:"ImdbID"`
}

var knownIncorrectOMDBMatches = map[string]struct{}{
	"obsession_2025": {},
	"backrooms_2026": {},
}

// LoadOMDBCache loads the OMDB cache from CSV file
func LoadOMDBCache() map[string]Entry {
	cache := make(map[string]Entry)

	file, err := os.Open("omdb_cache.csv")
	if err != nil {
		return cache // Return empty cache if file doesn't exist
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Printf("Error reading OMDB cache: %v", err)
		return cache
	}

	if len(records) < 2 {
		return cache // No data rows
	}

	for _, record := range records[1:] { // Skip header
		if len(record) >= 11 {
			key := NormalizeTitle(record[0]) + "_" + record[1]
			cache[key] = Entry{
				Title:          record[0],
				Year:           record[1],
				Poster:         record[2],
				Director:       record[3],
				Actors:         record[4],
				Plot:           record[5],
				Genre:          record[6],
				ImdbRating:     record[7],
				Metascore:      record[8],
				RottenTomatoes: record[9],
				ImdbID:         record[10],
			}
		}
	}

	log.Printf("Loaded %d entries from OMDB cache", len(cache))
	return cache
}

// SaveOMDBCache saves the OMDB cache to CSV file
func SaveOMDBCache(cache map[string]Entry) {
	file, err := os.Create("omdb_cache.csv")
	if err != nil {
		log.Printf("Error creating OMDB cache file: %v", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"Title", "Year", "Poster", "Director", "Actors", "Plot", "Genre", "ImdbRating", "Metascore", "RottenTomatoes", "ImdbID"}
	if err := writer.Write(header); err != nil {
		log.Printf("Error writing header to OMDB cache: %v", err)
		return
	}

	// Write data
	for _, entry := range cache {
		record := []string{
			entry.Title,
			entry.Year,
			entry.Poster,
			entry.Director,
			entry.Actors,
			entry.Plot,
			entry.Genre,
			entry.ImdbRating,
			entry.Metascore,
			entry.RottenTomatoes,
			entry.ImdbID,
		}
		if err := writer.Write(record); err != nil {
			log.Printf("Error writing record to OMDB cache: %v", err)
		}
	}

	log.Printf("Saved %d entries to OMDB cache", len(cache))
}

// NormalizeTitle normalizes movie titles for consistent cache lookup
func NormalizeTitle(title string) string {
	// Replace various dash characters with standard hyphen
	title = strings.ReplaceAll(title, "–", "-") // en-dash
	title = strings.ReplaceAll(title, "—", "-") // em-dash
	title = strings.ReplaceAll(title, "―", "-") // horizontal bar
	return strings.ToLower(strings.TrimSpace(title))
}

// EnrichMoviesWithCache enriches movies using cache first, then API for missing data
func EnrichMoviesWithCache(movies []reader.Movie, client *omdb.Client, cache map[string]Entry) {
	var cacheHits, cacheMisses, apiCalls, apiFails int

	for i := range movies {
		key := NormalizeTitle(movies[i].Name) + "_" + strconv.Itoa(movies[i].Year)
		if _, knownIncorrect := knownIncorrectOMDBMatches[key]; knownIncorrect {
			log.Printf("Skipping known-incorrect OMDB match for %s (%d)", movies[i].Name, movies[i].Year)
			continue
		}

		if entry, exists := cache[key]; exists {
			// Use cached data
			setOMDBPoster(&movies[i], entry.Poster)
			movies[i].Director = entry.Director
			movies[i].Actors = entry.Actors
			movies[i].Plot = entry.Plot
			movies[i].Genre = entry.Genre
			movies[i].ImdbRating = entry.ImdbRating
			movies[i].Metascore = entry.Metascore
			movies[i].RottenTomatoes = entry.RottenTomatoes
			movies[i].ImdbID = entry.ImdbID
			cacheHits++
		} else {
			// Fetch from API
			omdbMovie, err := client.GetMovieByTitleAndYear(movies[i].Name, movies[i].Year)
			if err != nil {
				log.Printf("Error fetching OMDB data for %s (%d): %v", movies[i].Name, movies[i].Year, err)
				apiFails++
				continue
			}

			apiCalls++

			// Enrich movie data
			setOMDBPoster(&movies[i], omdbMovie.Poster)
			movies[i].Director = omdbMovie.Director
			movies[i].Actors = omdbMovie.Actors
			movies[i].Plot = omdbMovie.Plot
			movies[i].Genre = omdbMovie.Genre
			movies[i].ImdbRating = omdbMovie.ImdbRating
			movies[i].Metascore = omdbMovie.Metascore
			movies[i].ImdbID = omdbMovie.ImdbID

			// Extract Rotten Tomatoes rating from Ratings array
			for _, rating := range omdbMovie.Ratings {
				if rating.Source == "Rotten Tomatoes" {
					movies[i].RottenTomatoes = rating.Value
					break
				}
			}

			// Add to cache
			cache[key] = Entry{
				Title:          omdbMovie.Title,
				Year:           omdbMovie.Year,
				Poster:         omdbMovie.Poster,
				Director:       omdbMovie.Director,
				Actors:         omdbMovie.Actors,
				Plot:           omdbMovie.Plot,
				Genre:          omdbMovie.Genre,
				ImdbRating:     omdbMovie.ImdbRating,
				Metascore:      omdbMovie.Metascore,
				RottenTomatoes: movies[i].RottenTomatoes,
				ImdbID:         omdbMovie.ImdbID,
			}
		}
	}

	// Calculate cache efficiency
	total := cacheHits + cacheMisses + apiCalls
	if total > 0 {
		efficiency := float64(cacheHits) / float64(total) * 100
		log.Printf("Cache efficiency: %.1f%% (%d hits, %d API calls, %d failures)", efficiency, cacheHits, apiCalls, apiFails)
	}
}

// EnrichMoviesFromCacheOnly enriches movies using only cached data (no API calls)
func EnrichMoviesFromCacheOnly(movies []reader.Movie, cache map[string]Entry) {
	var cacheHits, cacheMisses int

	for i := range movies {
		key := NormalizeTitle(movies[i].Name) + "_" + strconv.Itoa(movies[i].Year)
		if _, knownIncorrect := knownIncorrectOMDBMatches[key]; knownIncorrect {
			log.Printf("Skipping known-incorrect OMDB match for %s (%d)", movies[i].Name, movies[i].Year)
			continue
		}

		if entry, exists := cache[key]; exists {
			// Use cached data
			setOMDBPoster(&movies[i], entry.Poster)
			movies[i].Director = entry.Director
			movies[i].Actors = entry.Actors
			movies[i].Plot = entry.Plot
			movies[i].Genre = entry.Genre
			movies[i].ImdbRating = entry.ImdbRating
			movies[i].Metascore = entry.Metascore
			movies[i].RottenTomatoes = entry.RottenTomatoes
			movies[i].ImdbID = entry.ImdbID
			cacheHits++
		} else {
			cacheMisses++
		}
	}

	log.Printf("Cache-only enrichment: %d hits, %d misses", cacheHits, cacheMisses)
}

func setOMDBPoster(movie *reader.Movie, poster string) {
	if poster == "" || poster == "N/A" {
		return
	}
	if movie.Poster != "" && movie.Poster != poster {
		movie.FallbackPoster = movie.Poster
	}
	movie.Poster = poster
}
