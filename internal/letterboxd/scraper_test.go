package letterboxd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/seventy-two/boxy/internal/reader"
)

func TestParseReviewsPage(t *testing.T) {
	markup := `<html><body>
		<div class="listitem js-listitem" data-object-id="viewing:12345">
			<div class="react-component figure" data-component-class="LazyPoster" data-item-slug="example-film" data-resolvable-poster-path='{&#034;posteredBaseLink&#034;:&#034;/film/example-film/&#034;,&#034;cacheBustingKey&#034;:&#034;ab12cd&#034;}'>
				<img src="https://s.ltrbxd.com/static/img/empty-poster-70.png" alt="Example Film" />
			</div>
			<div class="inline-production-masthead">
				<h2 class="primaryname prettify"><a href="/dongdominator/film/example-film/">Example Film</a></h2>
				<span class="releasedate">2001</span>
			</div>
			<svg class="glyph -rating" aria-label="★★★½"></svg>
			<a href="/dongdominator/film/example-film/">Watched</a>
			<time class="timestamp" datetime="2026-05-10">10 May 2026</time>
			<div class="body-text js-review-body"><p>First paragraph.</p><p>Second paragraph.</p></div>
		</div>
		<a class="next" href="/dongdominator/reviews/films/page/2/">Next</a>
	</body></html>`
	document, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	pageURL, _ := url.Parse("https://letterboxd.com/dongdominator/reviews/")

	movies, nextURL := parseReviewsPage(document, pageURL)
	if len(movies) != 1 {
		t.Fatalf("got %d reviews, want 1", len(movies))
	}
	listing := movies[0]
	movie := listing.movie
	if listing.viewingID != "12345" {
		t.Errorf("unexpected viewing ID: %q", listing.viewingID)
	}
	if movie.Name != "Example Film" || movie.Year != 2001 || movie.Rating != 3.5 {
		t.Fatalf("unexpected movie metadata: %+v", movie)
	}
	if listing.posterMetadataURL != "https://letterboxd.com/film/example-film/poster/std/150/?k=ab12cd" {
		t.Errorf("unexpected Letterboxd poster metadata URL: %q", listing.posterMetadataURL)
	}
	if movie.Review != "First paragraph.\nSecond paragraph." {
		t.Errorf("unexpected review text: %q", movie.Review)
	}
	if movie.WatchedDate.Format("2006-01-02") != "2026-05-10" {
		t.Errorf("unexpected watched date: %s", movie.WatchedDate)
	}
	if movie.LetterboxdURI != "https://letterboxd.com/dongdominator/film/example-film/" {
		t.Errorf("unexpected Letterboxd URI: %q", movie.LetterboxdURI)
	}
	if nextURL == nil || nextURL.Path != "/dongdominator/reviews/films/page/2/" {
		t.Errorf("unexpected next page: %v", nextURL)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTripper roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func TestDoRequestUsesProxyOnlyAfterForbiddenResponse(t *testing.T) {
	directCalls := 0
	proxyCalls := 0
	scraper := &Scraper{
		client: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			directCalls++
			return &http.Response{StatusCode: http.StatusForbidden, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
		})},
		proxyClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			proxyCalls++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})},
	}
	request, err := http.NewRequest(http.MethodGet, "https://letterboxd.com/dongdominator/diary/", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := scraper.doRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if directCalls != 1 || proxyCalls != 1 || response.StatusCode != http.StatusOK {
		t.Fatalf("direct calls=%d, proxy calls=%d, status=%d; want 1, 1, %d", directCalls, proxyCalls, response.StatusCode, http.StatusOK)
	}
}

func TestDoRequestDoesNotUseProxyForSuccessfulDirectResponse(t *testing.T) {
	proxyCalls := 0
	scraper := &Scraper{
		client: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})},
		proxyClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			proxyCalls++
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("proxy"))}, nil
		})},
	}
	request, err := http.NewRequest(http.MethodGet, "https://letterboxd.com/dongdominator/diary/", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := scraper.doRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if proxyCalls != 0 {
		t.Fatalf("proxy called %d times for successful direct response, want 0", proxyCalls)
	}
}

func TestFetchLatestEntriesDoesNotFetchOlderPages(t *testing.T) {
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests[request.URL.Path]++
		switch request.URL.Path {
		case "/dongdominator/diary/":
			_, _ = io.WriteString(response, `<table><tr class="diary-entry-row" data-viewing-id="1"><td><a class="daydate" href="/dongdominator/diary/films/for/2026/10/07/">7</a></td><td><h2 class="primaryname">Recent Film</h2><span class="releasedate">2026</span></td><td class="col-rating"><input class="rateit-field" value="7" /></td></tr></table><a class="next" href="/dongdominator/diary/films/page/2/">Next</a>`)
		case "/dongdominator/reviews/":
			_, _ = io.WriteString(response, `<a class="next" href="/dongdominator/reviews/films/page/2/">Next</a>`)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	diaryURL, _ := url.Parse(server.URL + "/dongdominator/diary/")
	reviewsURL, _ := url.Parse(server.URL + "/dongdominator/reviews/")
	scraper := &Scraper{diaryURL: diaryURL, reviewsURL: reviewsURL, client: server.Client()}

	entries, err := scraper.FetchLatestEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "Recent Film" || entries[0].Rating != 3.5 {
		t.Fatalf("unexpected latest entries: %+v", entries)
	}
	if requests["/dongdominator/diary/films/page/2/"] != 0 || requests["/dongdominator/reviews/films/page/2/"] != 0 {
		t.Fatalf("scraper followed historical pagination: %v", requests)
	}
}

func TestParseDiaryPageIncludesReviewlessLogsAndMergesReviews(t *testing.T) {
	markup := `<html><body>
		<table><tbody>
			<tr class="diary-entry-row viewing-poster-container" data-viewing-id="101" data-object-id="viewing:101">
				<td><div class="react-component figure" data-component-class="LazyPoster" data-resolvable-poster-path='{&#034;posteredBaseLink&#034;:&#034;/film/click/&#034;,&#034;cacheBustingKey&#034;:&#034;abc123&#034;}'></div></td>
				<td class="col-daydate"><a class="daydate" href="/dongdominator/diary/films/for/2026/06/20/">20</a></td>
				<td><h2 class="primaryname prettify"><a href="/dongdominator/film/click/">Click</a></h2><span class="releasedate"><a href="/films/year/2006/">2006</a></span></td>
				<td class="col-rating"><input class="rateit-field" type="range" value="7" /></td>
				<td class="col-rewatch js-td-rewatch icon-status-on"><span class="has-icon icon-rewatch icon-16"></span></td>
			</tr>
			<tr class="diary-entry-row viewing-poster-container" data-viewing-id="102">
				<td class="col-daydate"><a class="daydate" href="/dongdominator/diary/films/for/2026/06/19/">19</a></td>
				<td><h2 class="primaryname prettify"><a href="/dongdominator/film/second-film/">Second Film</a></h2><span class="releasedate">2024</span></td>
				<td class="col-rewatch js-td-rewatch icon-status-off"><span class="has-icon icon-rewatch icon-16"></span></td>
			</tr>
		</tbody></table>
		<a class="next" href="/dongdominator/diary/films/page/2/">Older</a>
	</body></html>`
	document, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	pageURL, _ := url.Parse("https://letterboxd.com/dongdominator/diary/")

	entries, nextURL := parseDiaryPage(document, pageURL)
	if len(entries) != 2 {
		t.Fatalf("got %d diary entries, want 2", len(entries))
	}
	if entries[0].movie.Name != "Click" || entries[0].movie.Rating != 3.5 || !entries[0].movie.Rewatch {
		t.Errorf("unexpected first diary entry: %+v", entries[0].movie)
	}
	if entries[0].movie.WatchedDate.Format("2006-01-02") != "2026-06-20" {
		t.Errorf("unexpected watched date: %s", entries[0].movie.WatchedDate)
	}
	if entries[1].movie.Name != "Second Film" || entries[1].movie.Rating != 0 || entries[1].movie.Review != "" {
		t.Errorf("unexpected reviewless log: %+v", entries[1].movie)
	}
	if nextURL == nil || nextURL.Path != "/dongdominator/diary/films/page/2/" {
		t.Errorf("unexpected diary next page: %v", nextURL)
	}

	mergeReviewTexts(entries, []reviewListing{{viewingID: "101", movie: reader.Movie{Review: "A written review."}}})
	if entries[0].movie.Review != "A written review." {
		t.Errorf("review was not merged: %q", entries[0].movie.Review)
	}
	if entries[1].movie.Review != "" {
		t.Errorf("reviewless diary entry received review text: %q", entries[1].movie.Review)
	}
}
