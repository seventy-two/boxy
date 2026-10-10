package omdb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// Client represents an OMDB API client
type Client struct {
	APIKey  string
	BaseURL string
}

// NewClient creates a new OMDB API client
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		APIKey:  apiKey,
		BaseURL: baseURL,
	}
}

// GetMovieByTitleAndYear fetches movie data from OMDB API by title and year
func (c *Client) GetMovieByTitleAndYear(title string, year int) (*Movie, error) {
	params := url.Values{}
	params.Add("apikey", c.APIKey)
	params.Add("t", title)
	params.Add("y", strconv.Itoa(year))
	params.Add("plot", "full")

	resp, err := http.Get(c.BaseURL + "?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var movie Movie
	if err := json.Unmarshal(body, &movie); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if movie.Response == "False" {
		return nil, fmt.Errorf("movie not found: %s", movie.Error)
	}

	return &movie, nil
}
