package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/seventy-two/boxy/internal/reader"
)

// PageData represents the data passed to the template
type PageData struct {
	Movies         []MovieData `json:"movies"`
	TotalMovies    int         `json:"total_movies"`
	MoviesWithOMDB int         `json:"movies_with_omdb"`
	OMDBPercentage float64     `json:"omdb_percentage"`
	WithOMDB       bool        `json:"with_omdb"`
}

// MovieData represents a movie with searchable text
type MovieData struct {
	reader.Movie
	SearchableText string `json:"searchable_text"`
}

// HomeHandler handles the main web page
func HomeHandler(movieSnapshot func() []reader.Movie, hasOMDBClient bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Create template with custom function
		funcMap := template.FuncMap{
			"formatRating": func(rating float64) string {
				if rating == float64(int(rating)) {
					return fmt.Sprintf("%.0f", rating)
				}
				return fmt.Sprintf("%.1f", rating)
			},
		}

		tmpl, err := template.New("home.html").Funcs(funcMap).ParseFiles("templates/home.html")
		if err != nil {
			http.Error(w, "Error parsing template: "+err.Error(), http.StatusInternalServerError)
			return
		}

		data := preparePageData(movieSnapshot(), hasOMDBClient)

		if err := tmpl.Execute(w, data); err != nil {
			http.Error(w, "Error executing template: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// ApiHandler handles the JSON API endpoint
func ApiHandler(movieSnapshot func() []reader.Movie, hasOMDBClient bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		data := preparePageData(movieSnapshot(), hasOMDBClient)

		if err := json.NewEncoder(w).Encode(data); err != nil {
			http.Error(w, "Error encoding JSON: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

// preparePageData prepares the common page data for both HTML and JSON responses
func preparePageData(movies []reader.Movie, hasOMDBClient bool) PageData {
	// Calculate statistics
	moviesWithOMDB := 0
	for _, movie := range movies {
		if movie.Director != "" {
			moviesWithOMDB++
		}
	}

	omdbPercentage := 0.0
	if len(movies) > 0 {
		omdbPercentage = float64(moviesWithOMDB) / float64(len(movies)) * 100
	}

	// Convert movies to MovieData with searchable text
	var movieData []MovieData
	for _, movie := range movies {
		searchableText := fmt.Sprintf("%s %s %s %s %s %s %d",
			strings.ToLower(movie.Name),
			strings.ToLower(movie.Director),
			strings.ToLower(movie.Actors),
			strings.ToLower(movie.Genre),
			strings.ToLower(movie.Tags),
			strings.ToLower(movie.Plot),
			movie.Year,
		)

		movieData = append(movieData, MovieData{
			Movie:          movie,
			SearchableText: searchableText,
		})
	}

	return PageData{
		Movies:         movieData,
		TotalMovies:    len(movies),
		MoviesWithOMDB: moviesWithOMDB,
		OMDBPercentage: omdbPercentage,
		WithOMDB:       hasOMDBClient,
	}
}
