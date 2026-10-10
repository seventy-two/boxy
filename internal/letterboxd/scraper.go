package letterboxd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/seventy-two/boxy/internal/reader"
)

const firefoxUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

type Scraper struct {
	diaryURL    *url.URL
	reviewsURL  *url.URL
	client      *http.Client
	proxyClient *http.Client
	posterMu    sync.RWMutex
	posterURLs  map[string]string
}

func NewScraper(username string) *Scraper {
	diaryURL := &url.URL{
		Scheme: "https",
		Host:   "letterboxd.com",
		Path:   "/" + url.PathEscape(username) + "/diary/",
	}
	reviewsURL := *diaryURL
	reviewsURL.Path = "/" + url.PathEscape(username) + "/reviews/"
	return &Scraper{
		diaryURL:    diaryURL,
		reviewsURL:  &reviewsURL,
		client:      &http.Client{Timeout: 20 * time.Second},
		proxyClient: newProxyClient(os.Getenv("LETTERBOXD_PROXY_URL")),
		posterURLs:  make(map[string]string),
	}
}

func newProxyClient(proxyAddress string) *http.Client {
	if strings.TrimSpace(proxyAddress) == "" {
		return nil
	}

	proxyURL, err := url.Parse(proxyAddress)
	if err != nil || proxyURL.Host == "" || (proxyURL.Scheme != "http" && proxyURL.Scheme != "https") {
		log.Printf("Ignoring invalid LETTERBOXD_PROXY_URL")
		return nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}
}

type reviewListing struct {
	movie             reader.Movie
	posterMetadataURL string
	viewingID         string
}

func (s *Scraper) FetchLatestEntries(ctx context.Context) ([]reader.Movie, error) {
	diaryEntries, err := s.fetchPage(ctx, s.diaryURL, parseDiaryPage)
	if err != nil {
		return nil, fmt.Errorf("fetch Letterboxd diary: %w", err)
	}
	if len(diaryEntries) == 0 {
		return nil, fmt.Errorf("no diary entries found at %s", s.diaryURL)
	}

	reviews, err := s.fetchPage(ctx, s.reviewsURL, parseReviewsPage)
	if err != nil {
		log.Printf("Failed to fetch latest Letterboxd reviews; retaining cached review text: %v", err)
	} else {
		mergeReviewTexts(diaryEntries, reviews)
	}
	return s.resolvePosterURLs(ctx, diaryEntries)
}

type pageParser func(*html.Node, *url.URL) ([]reviewListing, *url.URL)

func (s *Scraper) fetchPage(ctx context.Context, pageURL *url.URL, parsePage pageParser) ([]reviewListing, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Letterboxd request: %w", err)
	}
	request.Header.Set("User-Agent", firefoxUserAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	request.Header.Set("Accept-Language", "en-US,en;q=0.5")

	response, err := s.doRequest(request)
	if err != nil {
		return nil, fmt.Errorf("fetch Letterboxd page %s: %w", pageURL, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("Letterboxd returned %s for %s", response.Status, pageURL)
	}
	defer response.Body.Close()

	document, err := html.Parse(response.Body)
	if err != nil {
		return nil, fmt.Errorf("parse Letterboxd page %s: %w", pageURL, err)
	}
	listings, _ := parsePage(document, pageURL)
	return listings, nil
}

func (s *Scraper) doRequest(request *http.Request) (*http.Response, error) {
	response, err := s.client.Do(request)
	if err != nil || response == nil || response.StatusCode != http.StatusForbidden || s.proxyClient == nil {
		return response, err
	}
	response.Body.Close()
	log.Printf("Letterboxd returned 403 for %s; retrying via configured proxy", request.URL)
	return s.proxyClient.Do(request.Clone(request.Context()))
}

func parseDiaryPage(document *html.Node, pageURL *url.URL) ([]reviewListing, *url.URL) {
	var entries []reviewListing
	var nextURL *url.URL

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if node.Data == "tr" && hasClass(node, "diary-entry-row") {
				if entry, ok := parseDiaryEntry(node, pageURL); ok {
					entries = append(entries, entry)
				}
			}
			if node.Data == "a" && hasClass(node, "next") && nextURL == nil {
				if href := attribute(node, "href"); href != "" {
					resolved := pageURL.ResolveReference(&url.URL{Path: href})
					if resolved.Host == pageURL.Host {
						nextURL = resolved
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)

	return entries, nextURL
}

func parseReviewsPage(document *html.Node, pageURL *url.URL) ([]reviewListing, *url.URL) {
	var reviews []reviewListing
	var nextURL *url.URL

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if hasClass(node, "listitem") && hasClass(node, "js-listitem") {
				if review, ok := parseReview(node, pageURL); ok {
					reviews = append(reviews, review)
				}
			}
			if node.Data == "a" && hasClass(node, "next") && nextURL == nil {
				if href := attribute(node, "href"); href != "" {
					resolved := pageURL.ResolveReference(&url.URL{Path: href})
					if resolved.Host == pageURL.Host {
						nextURL = resolved
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)

	return reviews, nextURL
}

func parseDiaryEntry(entry *html.Node, pageURL *url.URL) (reviewListing, bool) {
	var listing reviewListing
	movie := &listing.movie
	titleNode := findClass(entry, "primaryname")
	yearNode := findClass(entry, "releasedate")
	if titleNode == nil || yearNode == nil {
		return listing, false
	}

	movie.Name = strings.TrimSpace(textContent(titleNode))
	movie.Year, _ = strconv.Atoi(strings.TrimSpace(textContent(yearNode)))
	movie.Rating = parseRatingFromEntry(entry)
	if rewatchCell := findElement(entry, "td", "js-td-rewatch"); rewatchCell != nil {
		movie.Rewatch = hasClass(rewatchCell, "icon-status-on")
	}
	if dateLink := findElement(entry, "a", "daydate"); dateLink != nil {
		if watchedDate, ok := parseDiaryDate(attribute(dateLink, "href")); ok {
			movie.Date = watchedDate
			movie.WatchedDate = watchedDate
		}
	}
	if link := findProfileFilmLink(entry); link != "" {
		movie.LetterboxdURI = pageURL.ResolveReference(&url.URL{Path: link}).String()
	}
	listing.posterMetadataURL = letterboxdPosterMetadataURL(entry)
	listing.viewingID = findViewingID(entry)

	return listing, movie.Name != "" && movie.Year != 0
}

func parseDiaryDate(path string) (time.Time, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for index, part := range parts {
		if part == "for" && len(parts) >= index+4 {
			date, err := time.Parse("2006/01/02", strings.Join(parts[index+1:index+4], "/"))
			return date, err == nil
		}
	}
	return time.Time{}, false
}

func parseRatingFromEntry(entry *html.Node) float64 {
	if ratingInput := findElement(entry, "input", "rateit-field"); ratingInput != nil {
		if rating, err := strconv.Atoi(attribute(ratingInput, "value")); err == nil {
			return float64(rating) / 2
		}
	}
	if ratingNode := findElement(entry, "svg", "glyph -rating"); ratingNode != nil {
		return parseRating(attribute(ratingNode, "aria-label"))
	}
	return 0
}

func mergeReviewTexts(diaryEntries, reviews []reviewListing) {
	reviewByViewingID := make(map[string]string, len(reviews))
	for _, review := range reviews {
		if review.viewingID != "" && review.movie.Review != "" {
			reviewByViewingID[review.viewingID] = review.movie.Review
		}
	}
	for index := range diaryEntries {
		if review, ok := reviewByViewingID[diaryEntries[index].viewingID]; ok {
			diaryEntries[index].movie.Review = review
		}
	}
}

func parseReview(entry *html.Node, pageURL *url.URL) (reviewListing, bool) {
	var listing reviewListing
	movie := &listing.movie
	titleNode := findClass(entry, "primaryname")
	yearNode := findClass(entry, "releasedate")
	reviewNode := findClass(entry, "js-review-body")
	dateNode := findElement(entry, "time", "timestamp")
	if titleNode == nil || yearNode == nil || reviewNode == nil {
		return listing, false
	}

	movie.Name = strings.TrimSpace(textContent(titleNode))
	movie.Year, _ = strconv.Atoi(strings.TrimSpace(textContent(yearNode)))
	movie.Review = strings.TrimSpace(textContent(reviewNode))
	listing.posterMetadataURL = letterboxdPosterMetadataURL(entry)
	if dateNode != nil {
		if watchedDate, err := time.Parse("2006-01-02", attribute(dateNode, "datetime")); err == nil {
			movie.Date = watchedDate
			movie.WatchedDate = watchedDate
		}
	}
	movie.Rating = parseRatingFromEntry(entry)
	if link := findProfileFilmLink(entry); link != "" {
		movie.LetterboxdURI = pageURL.ResolveReference(&url.URL{Path: link}).String()
	}
	listing.viewingID = findViewingID(entry)

	return listing, movie.Name != "" && movie.Year != 0
}

func findViewingID(node *html.Node) string {
	if viewingID := attribute(node, "data-viewing-id"); viewingID != "" {
		return viewingID
	}
	if objectID := attribute(node, "data-object-id"); objectID != "" {
		return strings.TrimPrefix(objectID, "viewing:")
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if viewingID := findViewingID(child); viewingID != "" {
			return viewingID
		}
	}
	return ""
}

func letterboxdPosterMetadataURL(entry *html.Node) string {
	posterNode := findComponent(entry, "LazyPoster")
	if posterNode == nil {
		return ""
	}

	var posterPath struct {
		BaseLink        string `json:"posteredBaseLink"`
		CacheBustingKey string `json:"cacheBustingKey"`
	}
	if err := json.Unmarshal([]byte(attribute(posterNode, "data-resolvable-poster-path")), &posterPath); err != nil {
		return ""
	}
	baseLink := posterPath.BaseLink
	if !strings.HasPrefix(baseLink, "/film/") || strings.ContainsAny(baseLink, "?#") || strings.Contains(baseLink, "..") {
		return ""
	}
	metadataURL := &url.URL{
		Scheme: "https",
		Host:   "letterboxd.com",
		Path:   strings.TrimRight(baseLink, "/") + "/poster/std/150/",
	}
	if posterPath.CacheBustingKey != "" {
		query := metadataURL.Query()
		query.Set("k", posterPath.CacheBustingKey)
		metadataURL.RawQuery = query.Encode()
	}
	return metadataURL.String()
}

func (s *Scraper) resolvePosterURLs(ctx context.Context, listings []reviewListing) ([]reader.Movie, error) {
	const workers = 6
	jobs := make(chan int)
	var wait sync.WaitGroup

	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				metadataURL := listings[index].posterMetadataURL
				if metadataURL == "" {
					continue
				}
				if posterURL := s.cachedPosterURL(metadataURL); posterURL != "" {
					listings[index].movie.Poster = posterURL
					continue
				}
				posterURL, err := s.fetchPosterURL(ctx, metadataURL)
				if err == nil {
					listings[index].movie.Poster = posterURL
					s.cachePosterURL(metadataURL, posterURL)
				}
			}
		}()
	}

	for index := range listings {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wait.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wait.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	movies := make([]reader.Movie, len(listings))
	for index := range listings {
		movies[index] = listings[index].movie
	}
	return movies, nil
}

func (s *Scraper) fetchPosterURL(ctx context.Context, metadataURL string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return "", fmt.Errorf("create poster metadata request: %w", err)
	}
	request.Header.Set("User-Agent", "Boxy personal movie diary")
	request.Header.Set("Accept", "application/json")

	response, err := s.doRequest(request)
	if err != nil {
		return "", fmt.Errorf("fetch poster metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Letterboxd poster metadata returned %s", response.Status)
	}

	var metadata struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode poster metadata: %w", err)
	}
	posterURL, err := url.Parse(metadata.URL)
	if err != nil || posterURL.Scheme != "https" || !strings.HasSuffix(posterURL.Host, ".ltrbxd.com") {
		return "", fmt.Errorf("invalid Letterboxd poster URL %q", metadata.URL)
	}
	return posterURL.String(), nil
}

func (s *Scraper) cachedPosterURL(metadataURL string) string {
	s.posterMu.RLock()
	defer s.posterMu.RUnlock()
	return s.posterURLs[metadataURL]
}

func (s *Scraper) cachePosterURL(metadataURL, posterURL string) {
	s.posterMu.Lock()
	defer s.posterMu.Unlock()
	if s.posterURLs == nil {
		s.posterURLs = make(map[string]string)
	}
	s.posterURLs[metadataURL] = posterURL
}

func parseRating(label string) float64 {
	return float64(strings.Count(label, "★")) + 0.5*float64(strings.Count(label, "½"))
}

func findProfileFilmLink(node *html.Node) string {
	var result string
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if result != "" {
			return
		}
		if current.Type == html.ElementNode && current.Data == "a" {
			href := attribute(current, "href")
			if strings.Contains(href, "/film/") && !strings.Contains(href, "/films/year/") {
				result = href
				return
			}
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return result
}

func findClass(node *html.Node, class string) *html.Node {
	if hasClass(node, class) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findClass(child, class); found != nil {
			return found
		}
	}
	return nil
}

func findElement(node *html.Node, tag, class string) *html.Node {
	if node.Type == html.ElementNode && node.Data == tag && hasClass(node, class) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag, class); found != nil {
			return found
		}
	}
	return nil
}

func findComponent(node *html.Node, component string) *html.Node {
	if node.Type == html.ElementNode && attribute(node, "data-component-class") == component {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findComponent(child, component); found != nil {
			return found
		}
	}
	return nil
}

func hasClass(node *html.Node, class string) bool {
	return node.Type == html.ElementNode && strings.Contains(" "+attribute(node, "class")+" ", " "+class+" ")
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func textContent(node *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
			return
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
			if child.Type == html.ElementNode && (child.Data == "p" || child.Data == "br") {
				text.WriteByte('\n')
			}
		}
	}
	walk(node)
	return text.String()
}
