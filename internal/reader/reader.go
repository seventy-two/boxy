package reader

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/seventy-two/boxy/internal/omdb"
)

// Movie represents a movie diary entry with OMDB data
type Movie struct {
	// CSV fields
	Date          time.Time `csv:"Date"`
	Name          string    `csv:"Name"`
	Year          int       `csv:"Year"`
	LetterboxdURI string    `csv:"Letterboxd URI"`
	Rating        float64   `csv:"Rating"`
	Rewatch       bool      `csv:"Rewatch"`
	Tags          string    `csv:"Tags"`
	WatchedDate   time.Time `csv:"Watched Date"`

	// Review field
	Review string `csv:"Review"`

	// OMDB fields
	Poster         string `json:"poster,omitempty"`
	FallbackPoster string `json:"fallback_poster,omitempty"`
	Director       string `json:"director,omitempty"`
	Actors         string `json:"actors,omitempty"`
	Plot           string `json:"plot,omitempty"`
	Genre          string `json:"genre,omitempty"`
	ImdbRating     string `json:"imdb_rating,omitempty"`
	Metascore      string `json:"metascore,omitempty"`
	RottenTomatoes string `json:"rotten_tomatoes,omitempty"`
	ImdbID         string `json:"imdb_id,omitempty"`
}

// ReadMovieDiary reads the diary.csv file and returns a slice of Movie structs
func ReadMovieDiary(filename string) ([]Movie, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("CSV file is empty")
	}

	// Skip header row
	records = records[1:]

	movies := make([]Movie, 0, len(records))
	for i, record := range records {
		if len(record) != 8 {
			return nil, fmt.Errorf("invalid record at line %d: expected 8 fields, got %d", i+2, len(record))
		}

		movie, err := parseMovieRecord(record)
		if err != nil {
			return nil, fmt.Errorf("failed to parse record at line %d: %w", i+2, err)
		}

		movies = append(movies, movie)
	}

	return movies, nil
}

// ReadMovieDiaryWithOMDB reads the diary.csv file and enriches it with OMDB data
func ReadMovieDiaryWithOMDB(filename string, omdbClient *omdb.Client) ([]Movie, error) {
	// First read the basic movie data
	movies, err := ReadMovieDiary(filename)
	if err != nil {
		return nil, err
	}

	if omdbClient == nil {
		log.Println("No OMDB client provided, skipping OMDB data enrichment")
		return movies, nil
	}

	// Enrich each movie with OMDB data
	for i, movie := range movies {
		log.Printf("Fetching OMDB data for: %s (%d) [%d/%d]", movie.Name, movie.Year, i+1, len(movies))

		omdbMovie, err := omdbClient.GetMovieByTitleAndYear(movie.Name, movie.Year)
		if err != nil {
			log.Printf("Failed to get OMDB data for %s (%d): %v", movie.Name, movie.Year, err)
			continue
		}

		// Enrich the movie with OMDB data
		movies[i].Poster = omdbMovie.Poster
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
	}

	return movies, nil
}

// parseMovieRecord converts a CSV record to a Movie struct
func parseMovieRecord(record []string) (Movie, error) {
	var movie Movie
	var err error

	// Parse Date
	movie.Date, err = time.Parse("2006-01-02", record[0])
	if err != nil {
		return movie, fmt.Errorf("invalid date format: %w", err)
	}

	// Parse Name
	movie.Name = record[1]

	// Parse Year
	movie.Year, err = strconv.Atoi(record[2])
	if err != nil {
		return movie, fmt.Errorf("invalid year: %w", err)
	}

	// Parse Letterboxd URI
	movie.LetterboxdURI = record[3]

	// Parse Rating
	if record[4] != "" {
		movie.Rating, err = strconv.ParseFloat(record[4], 64)
		if err != nil {
			return movie, fmt.Errorf("invalid rating: %w", err)
		}
	}

	// Parse Rewatch
	movie.Rewatch = record[5] == "Yes"

	// Parse Tags
	movie.Tags = record[6]

	// Parse Watched Date
	movie.WatchedDate, err = time.Parse("2006-01-02", record[7])
	if err != nil {
		return movie, fmt.Errorf("invalid watched date format: %w", err)
	}

	return movie, nil
}

// ReadMovieDiaryWithReviews reads both diary.csv and reviews.csv and merges them
func ReadMovieDiaryWithReviews(diaryFilename, reviewsFilename string) ([]Movie, error) {
	// Read diary movies first
	movies, err := ReadMovieDiary(diaryFilename)
	if err != nil {
		return nil, err
	}

	// Read reviews
	reviewMovies, err := readReviews(reviewsFilename)
	if err != nil {
		// If reviews file doesn't exist or has errors, just return diary movies
		log.Printf("Warning: Could not read reviews: %v", err)
		return movies, nil
	}

	// Create a map of reviews by movie key (name + year)
	reviewMap := make(map[string]string)
	for _, reviewMovie := range reviewMovies {
		key := fmt.Sprintf("%s_%d", reviewMovie.Name, reviewMovie.Year)
		reviewMap[key] = reviewMovie.Review
	}

	// Merge reviews into diary movies
	for i := range movies {
		key := fmt.Sprintf("%s_%d", movies[i].Name, movies[i].Year)
		if review, exists := reviewMap[key]; exists {
			movies[i].Review = review
		}
	}

	return movies, nil
}

func WriteMovieDiaryAndReviews(diaryFilename, reviewsFilename string, movies []Movie) error {
	diaryRecords := [][]string{{"Date", "Name", "Year", "Letterboxd URI", "Rating", "Rewatch", "Tags", "Watched Date"}}
	reviewRecords, err := readReviewCSVRecords(reviewsFilename)
	if err != nil {
		return fmt.Errorf("read existing reviews fallback: %w", err)
	}
	reviewIndices := make(map[string]int)
	for index, record := range reviewRecords[1:] {
		if key := reviewRecordKey(record); key != "" {
			reviewIndices[key] = index + 1
		}
	}

	for _, movie := range movies {
		date := movie.Date
		if date.IsZero() {
			date = movie.WatchedDate
		}
		watchedDate := movie.WatchedDate
		if watchedDate.IsZero() {
			watchedDate = date
		}
		rating := ""
		if movie.Rating != 0 {
			rating = strconv.FormatFloat(movie.Rating, 'f', -1, 64)
		}
		rewatch := "No"
		if movie.Rewatch {
			rewatch = "Yes"
		}
		base := []string{
			date.Format("2006-01-02"),
			movie.Name,
			strconv.Itoa(movie.Year),
			movie.LetterboxdURI,
			rating,
			rewatch,
		}
		diaryRecords = append(diaryRecords, append(append([]string{}, base...), movie.Tags, watchedDate.Format("2006-01-02")))
		if movie.Review != "" {
			reviewRecord := append(append([]string{}, base...), movie.Review, movie.Tags, watchedDate.Format("2006-01-02"))
			key := reviewRecordKey(reviewRecord)
			if index, exists := reviewIndices[key]; exists {
				reviewRecords[index] = reviewRecord
			} else {
				reviewIndices[key] = len(reviewRecords)
				reviewRecords = append(reviewRecords, reviewRecord)
			}
		}
	}

	if err := writeCSVAtomically(diaryFilename, diaryRecords); err != nil {
		return fmt.Errorf("write diary fallback: %w", err)
	}
	if err := writeCSVAtomically(reviewsFilename, reviewRecords); err != nil {
		return fmt.Errorf("write reviews fallback: %w", err)
	}
	return nil
}

func readReviewCSVRecords(filename string) ([][]string, error) {
	file, err := os.Open(filename)
	if os.IsNotExist(err) {
		return [][]string{{"Date", "Name", "Year", "Letterboxd URI", "Rating", "Rewatch", "Review", "Tags", "Watched Date"}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return [][]string{{"Date", "Name", "Year", "Letterboxd URI", "Rating", "Rewatch", "Review", "Tags", "Watched Date"}}, nil
	}
	return records, nil
}

func reviewRecordKey(record []string) string {
	if len(record) < 9 {
		return ""
	}
	date := record[8]
	if date == "" {
		date = record[0]
	}
	return strings.ToLower(strings.TrimSpace(record[1])) + "|" + record[2] + "|" + date
}

func writeCSVAtomically(filename string, records [][]string) error {
	temporary, err := os.CreateTemp(filepath.Dir(filename), "."+filepath.Base(filename)+"-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	writer := csv.NewWriter(temporary)
	if err := writer.WriteAll(records); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0644); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return err
	}
	return nil
}

// readReviews reads the reviews.csv file
func readReviews(filename string) ([]Movie, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open reviews file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read reviews CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("reviews CSV file is empty or has no data rows")
	}

	var movies []Movie
	for i, record := range records[1:] { // Skip header
		if len(record) < 9 { // Need at least 9 columns including Review
			log.Printf("Skipping row %d: insufficient columns", i+2)
			continue
		}

		movie, err := parseReviewRecord(record)
		if err != nil {
			log.Printf("Error parsing row %d: %v", i+2, err)
			continue
		}

		movies = append(movies, movie)
	}

	return movies, nil
}

// parseReviewRecord converts a reviews CSV record to a Movie struct (only parsing necessary fields)
func parseReviewRecord(record []string) (Movie, error) {
	var movie Movie
	var err error

	// Parse Name
	movie.Name = record[1]

	// Parse Year
	movie.Year, err = strconv.Atoi(record[2])
	if err != nil {
		return movie, fmt.Errorf("invalid year: %w", err)
	}

	// Parse Review (column 6)
	movie.Review = record[6]

	return movie, nil
}
