package letterboxd

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/seventy-two/boxy/internal/reader"
)

const firefoxUserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0"

// Scraper reads a member's diary from their Letterboxd RSS feed. Unlike the
// paginated diary and review pages, the feed isn't behind Cloudflare's
// challenge. It holds the latest 50 entries, including review text and
// poster, which is enough for a daily refresh on top of the saved history.
type Scraper struct {
	feedURL     *url.URL
	client      *http.Client
	proxyClient *http.Client
}

func NewScraper(username string) *Scraper {
	return &Scraper{
		feedURL: &url.URL{
			Scheme: "https",
			Host:   "letterboxd.com",
			Path:   "/" + url.PathEscape(username) + "/rss/",
		},
		client:      &http.Client{Timeout: 20 * time.Second},
		proxyClient: newProxyClient(os.Getenv("LETTERBOXD_PROXY_URL")),
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

// FetchLatestEntries returns the diary entries in the member's RSS feed.
func (s *Scraper) FetchLatestEntries(ctx context.Context) ([]reader.Movie, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.feedURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create Letterboxd request: %w", err)
	}
	request.Header.Set("User-Agent", firefoxUserAgent)
	request.Header.Set("Accept", "application/rss+xml,application/xml;q=0.9,*/*;q=0.8")

	response, err := s.doRequest(request)
	if err != nil {
		return nil, fmt.Errorf("fetch Letterboxd feed %s: %w", s.feedURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Letterboxd returned %s for %s", response.Status, s.feedURL)
	}

	var feed rssFeed
	if err := xml.NewDecoder(response.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("parse Letterboxd feed %s: %w", s.feedURL, err)
	}
	movies := parseFeed(feed)
	if len(movies) == 0 {
		return nil, fmt.Errorf("no diary entries found at %s", s.feedURL)
	}
	return movies, nil
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

type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

// rssItem is one feed entry. The letterboxd: fields are only present on
// diary entries, not on lists.
type rssItem struct {
	Link        string `xml:"link"`
	WatchedDate string `xml:"https://letterboxd.com watchedDate"`
	Rewatch     string `xml:"https://letterboxd.com rewatch"`
	FilmTitle   string `xml:"https://letterboxd.com filmTitle"`
	FilmYear    string `xml:"https://letterboxd.com filmYear"`
	Rating      string `xml:"https://letterboxd.com memberRating"`
	Description string `xml:"description"`
}

func parseFeed(feed rssFeed) []reader.Movie {
	var movies []reader.Movie
	for _, item := range feed.Items {
		watched, err := time.Parse("2006-01-02", strings.TrimSpace(item.WatchedDate))
		year, _ := strconv.Atoi(strings.TrimSpace(item.FilmYear))
		name := strings.TrimSpace(item.FilmTitle)
		if err != nil || name == "" || year == 0 {
			continue
		}
		rating, _ := strconv.ParseFloat(strings.TrimSpace(item.Rating), 64)
		poster, review := parseDescription(item.Description)

		movies = append(movies, reader.Movie{
			Date:          watched,
			WatchedDate:   watched,
			Name:          name,
			Year:          year,
			LetterboxdURI: strings.TrimSpace(item.Link),
			Rating:        rating,
			Rewatch:       strings.EqualFold(strings.TrimSpace(item.Rewatch), "Yes"),
			Review:        review,
			Poster:        poster,
		})
	}
	return movies
}

// parseDescription pulls the poster URL and review text out of an entry's
// HTML description. Besides the review's own paragraphs, it holds a poster
// paragraph, a "Watched on ..." line and, for some reviews, a spoiler
// warning.
func parseDescription(description string) (poster, review string) {
	document, err := html.Parse(strings.NewReader(description))
	if err != nil {
		return "", ""
	}

	if img := findElement(document, "img"); img != nil {
		poster = smallPoster(validPosterURL(attribute(img, "src")))
	}

	var paragraphs []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "p" {
			text := strings.TrimSpace(textContent(node))
			if text != "" && !strings.HasPrefix(text, "Watched on ") && text != "This review may contain spoilers." {
				paragraphs = append(paragraphs, text)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return poster, strings.Join(paragraphs, "\n")
}

func validPosterURL(raw string) string {
	posterURL, err := url.Parse(raw)
	if err != nil || posterURL.Scheme != "https" || !strings.HasSuffix(posterURL.Host, ".ltrbxd.com") {
		return ""
	}
	return posterURL.String()
}

// posterSize matches the size in a Letterboxd poster URL, such as the feed's
// "-0-600-0-900-crop".
var posterSize = regexp.MustCompile(`-0-\d+-0-\d+-crop\.`)

// smallPoster asks for the 150px-wide poster rather than the feed's 600px
// one; the page shows posters at 100px.
func smallPoster(poster string) string {
	return posterSize.ReplaceAllString(poster, "-0-150-0-225-crop.")
}

func findElement(node *html.Node, tag string) *html.Node {
	if node.Type == html.ElementNode && node.Data == tag {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag); found != nil {
			return found
		}
	}
	return nil
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
		if current.Type == html.ElementNode && current.Data == "br" {
			text.WriteByte('\n')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return text.String()
}
