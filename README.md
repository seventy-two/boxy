## Movie Diary Reader with OMDB Integration

This Go application scrapes your public Letterboxd diary and enriches it with additional movie information from the OMDB API. It refreshes once a day and uses the included CSV exports if Letterboxd is unavailable at startup.

### Features

- **Live Diary**: Refreshes the latest diary and review pages once a day
- **CSV Fallback**: Merges new diary entries into saved history and retains older reviews if Letterboxd blocks historical pages
- **OMDB Integration**: Fetches movie posters, directors, actors, plot summaries, and IMDB ratings
- **Error Handling**: Gracefully handles missing movies or API failures
- **Flexible Usage**: Works with or without OMDB API key

### Movie Data Structure

Each movie contains:

**From CSV:**
- Date, Name, Year
- Your Letterboxd rating
- Rewatch status
- Tags
- Watched date
- Letterboxd URI

**From OMDB API:**
- Poster image URL
- Director
- Cast (actors)
- Plot summary
- Genre
- IMDB rating
- IMDB ID

### Usage

#### Basic Usage
```bash
go run main.go
```

By default the app uses the `dongdominator` Letterboxd account. Set `LETTERBOXD_USERNAME` to use another public account.

Letterboxd requests are made directly by default. If a request receives HTTP 403 and `LETTERBOXD_PROXY_URL` is set to an HTTP or HTTPS proxy, that request is retried once through the proxy. Other requests remain direct.

```bash
export LETTERBOXD_PROXY_URL="http://proxy-host:port"
```

#### With OMDB Integration
1. Get a free API key from [OMDB API](http://www.omdbapi.com/)
2. Set the environment variable:
```bash
export OMDB_API_KEY="your_api_key_here"
go run main.go
```

### Example Output

**Without OMDB:**
```
Movie 1:
  Name: Constantine (2005)
  Your Rating: 3.5
  Watched: 2022-09-03
  Rewatch: false
  Letterboxd: https://boxd.it/3bRy25
```

**With OMDB:**
```
Movie 1:
  Name: Constantine (2005)
  Your Rating: 3.5
  Watched: 2022-09-03
  Rewatch: false
  Director: Francis Lawrence
  Actors: Keanu Reeves, Rachel Weisz, Djimon Hounsou
  Genre: Action, Fantasy, Horror
  IMDB Rating: 7.0
  Plot: Supernatural exorcist and demonologist John Constantine helps a policewoman prove her sister's death was not a suicide, but something more.
  Poster: https://m.media-amazon.com/images/M/MV5BNGJmZTUzYjMtN2Y2My...
  Letterboxd: https://boxd.it/3bRy25
```

### API Functions

#### `reader.ReadMovieDiary(filename)`
Reads basic CSV data only.

#### `reader.ReadMovieDiaryWithOMDB(filename, client)`  
Reads CSV data and enriches with OMDB information using provided client.

#### `omdb.NewClient(apiKey)`
Creates persistent OMDB API client that can be reused across multiple requests.

#### `client.GetMovieByTitleAndYear(title, year)`
Fetches individual movie data from OMDB using the persistent client.

### Error Handling

- Missing OMDB API key: Falls back to CSV-only mode
- Invalid API key: Shows error messages but continues processing
- Movie not found: Logs error and continues with next movie
- Network issues: Graceful degradation

### Performance Notes

- OMDB API has rate limits (1000 requests/day for free tier)
- Each movie requires one API call
- Progress is logged for large collections
- Failed requests don't stop the process

### Dependencies

- Standard Go library only
- No external dependencies for CSV reading
- HTTP client for OMDB API calls