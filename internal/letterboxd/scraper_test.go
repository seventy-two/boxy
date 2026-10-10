package letterboxd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testFeed = `<?xml version='1.0' encoding='utf-8'?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:letterboxd="https://letterboxd.com" xmlns:tmdb="https://themoviedb.org">
	<channel>
		<title>Letterboxd - Megz</title>
		<item>
			<title>Click, 2006 - ★★★½</title>
			<link>https://letterboxd.com/dongdominator/film/click/</link>
			<guid isPermaLink="false">letterboxd-review-1</guid>
			<letterboxd:watchedDate>2026-10-05</letterboxd:watchedDate>
			<letterboxd:rewatch>No</letterboxd:rewatch>
			<letterboxd:filmTitle>Click</letterboxd:filmTitle>
			<letterboxd:filmYear>2006</letterboxd:filmYear>
			<letterboxd:memberRating>3.5</letterboxd:memberRating>
			<description><![CDATA[ <p><img src="https://a.ltrbxd.com/resized/film-poster/click-0-600-0-900-crop.jpg?v=1"/></p> <p>They should make a sequel where it still goes wrong. I&#039;d watch that</p> <p>Second paragraph<br/>with a line break.</p> ]]></description>
		</item>
		<item>
			<title>The Fast and the Furious, 2001 - ★★★★★</title>
			<link>https://letterboxd.com/dongdominator/film/the-fast-and-the-furious-2001/1/</link>
			<guid isPermaLink="false">letterboxd-watch-2</guid>
			<letterboxd:watchedDate>2026-10-07</letterboxd:watchedDate>
			<letterboxd:rewatch>Yes</letterboxd:rewatch>
			<letterboxd:filmTitle>The Fast and the Furious</letterboxd:filmTitle>
			<letterboxd:filmYear>2001</letterboxd:filmYear>
			<letterboxd:memberRating>5.0</letterboxd:memberRating>
			<description><![CDATA[ <p><img src="https://a.ltrbxd.com/resized/film-poster/fast-0-600-0-900-crop.jpg?v=2"/></p> <p>Watched on Wednesday October 7, 2026.</p> ]]></description>
		</item>
		<item>
			<title>Heat, 1995</title>
			<link>https://letterboxd.com/dongdominator/film/heat-1995/</link>
			<guid isPermaLink="false">letterboxd-review-3</guid>
			<letterboxd:watchedDate>2026-09-01</letterboxd:watchedDate>
			<letterboxd:rewatch>No</letterboxd:rewatch>
			<letterboxd:filmTitle>Heat</letterboxd:filmTitle>
			<letterboxd:filmYear>1995</letterboxd:filmYear>
			<description><![CDATA[ <p><img src="https://a.ltrbxd.com/resized/film-poster/heat.jpg"/></p> <p><em>This review may contain spoilers.</em></p> <p>The bank job.</p> ]]></description>
		</item>
		<item>
			<title>Favourite heist films</title>
			<link>https://letterboxd.com/dongdominator/list/favourite-heist-films/</link>
			<guid isPermaLink="false">letterboxd-list-4</guid>
			<description><![CDATA[ <p>A list.</p> ]]></description>
		</item>
	</channel>
</rss>`

func TestFetchLatestEntriesReadsFeed(t *testing.T) {
	requests := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests[request.URL.Path]++
		if request.URL.Path != "/dongdominator/rss/" {
			http.NotFound(response, request)
			return
		}
		_, _ = io.WriteString(response, testFeed)
	}))
	defer server.Close()
	feedURL, _ := url.Parse(server.URL + "/dongdominator/rss/")
	scraper := &Scraper{feedURL: feedURL, client: server.Client()}

	movies, err := scraper.FetchLatestEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 {
		t.Fatalf("made requests other than the feed: %v", requests)
	}
	if len(movies) != 3 {
		t.Fatalf("got %d entries, want 3 (lists skipped): %+v", len(movies), movies)
	}

	click := movies[0]
	if click.Name != "Click" || click.Year != 2006 || click.Rating != 3.5 || click.Rewatch {
		t.Errorf("unexpected metadata: %+v", click)
	}
	if click.WatchedDate.Format("2006-01-02") != "2026-10-05" || !click.Date.Equal(click.WatchedDate) {
		t.Errorf("unexpected dates: %s / %s", click.Date, click.WatchedDate)
	}
	if click.LetterboxdURI != "https://letterboxd.com/dongdominator/film/click/" {
		t.Errorf("unexpected URI: %q", click.LetterboxdURI)
	}
	if click.Poster != "https://a.ltrbxd.com/resized/film-poster/click-0-150-0-225-crop.jpg?v=1" {
		t.Errorf("unexpected poster: %q", click.Poster)
	}
	if want := "They should make a sequel where it still goes wrong. I'd watch that\nSecond paragraph\nwith a line break."; click.Review != want {
		t.Errorf("review = %q, want %q", click.Review, want)
	}

	fast := movies[1]
	if !fast.Rewatch || fast.Rating != 5 || fast.Review != "" {
		t.Errorf("unexpected rewatch entry: %+v", fast)
	}

	heat := movies[2]
	if heat.Rating != 0 || heat.Review != "The bank job." {
		t.Errorf("unexpected unrated spoiler entry: rating=%v review=%q", heat.Rating, heat.Review)
	}
}

func TestFetchLatestEntriesErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"blocked": func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "challenge", http.StatusForbidden)
		},
		"no entries": func(response http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(response, `<rss><channel></channel></rss>`)
		},
		"not xml": func(response http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(response, `<html><body>Just a moment...`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			feedURL, _ := url.Parse(server.URL + "/dongdominator/rss/")
			scraper := &Scraper{feedURL: feedURL, client: server.Client()}
			if _, err := scraper.FetchLatestEntries(context.Background()); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestParseDescriptionRejectsForeignPosters(t *testing.T) {
	poster, review := parseDescription(`<p><img src="http://evil.example/x.jpg"/></p><p>Fine.</p>`)
	if poster != "" || review != "Fine." {
		t.Fatalf("poster=%q review=%q", poster, review)
	}
	if strings.Contains(review, "Watched on") {
		t.Fatal("kept the watched line")
	}
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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTripper roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}
