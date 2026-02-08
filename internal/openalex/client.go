package openalex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://api.openalex.org"

type Client struct {
	http   *http.Client
	email  string
	apiKey string
}

func NewClient(email, apiKey string) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		email:  email,
		apiKey: apiKey,
	}
}

func (c *Client) do(ctx context.Context, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	if c.apiKey != "" {
		params.Set("api_key", c.apiKey)
	}
	if c.email != "" {
		params.Set("mailto", c.email)
	}

	u := baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "science-newsletter/1.0")

	start := time.Now()
	resp, err := c.http.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		slog.Error("openalex request failed", "path", path, "duration", elapsed, "err", err)
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	slog.Debug("openalex request", "path", path, "status", resp.StatusCode, "duration", elapsed)

	if resp.StatusCode != http.StatusOK {
		slog.Error("openalex non-200 response", "path", path, "status", resp.StatusCode, "duration", elapsed)
		return fmt.Errorf("openalex: %s returned %d", path, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// SearchAuthors searches for authors by name.
func (c *Client) SearchAuthors(ctx context.Context, name string) ([]Author, error) {
	slog.Debug("searching authors", "query", name)
	params := url.Values{
		"search": {name},
	}
	var resp AuthorSearchResponse
	if err := c.do(ctx, "/authors", params, &resp); err != nil {
		return nil, err
	}
	slog.Debug("author search results", "query", name, "count", len(resp.Results))
	return resp.Results, nil
}

// GetAuthor fetches a single author by OpenAlex ID.
func (c *Client) GetAuthor(ctx context.Context, id string) (*Author, error) {
	slog.Debug("fetching author", "id", id)
	var author Author
	if err := c.do(ctx, "/authors/"+url.PathEscape(id), nil, &author); err != nil {
		return nil, err
	}
	slog.Debug("fetched author", "id", id, "name", author.DisplayName, "topics", len(author.Topics))
	return &author, nil
}

// GetAuthorWorks fetches works for an author using cursor pagination.
// If since is non-zero, only works published on or after that date are returned.
// It returns all matching works across all pages.
func (c *Client) GetAuthorWorks(ctx context.Context, authorID string, since time.Time) ([]Work, error) {
	slog.Info("fetching works for author", "author_id", authorID, "since", since)
	start := time.Now()
	var all []Work
	cursor := "*"
	page := 0

	filter := "authorships.author.id:" + authorID
	if !since.IsZero() {
		filter += ",from_publication_date:" + since.Format("2006-01-02")
	}

	for {
		page++
		params := url.Values{
			"filter":   {filter},
			"per_page": {"200"},
			"cursor":   {cursor},
		}
		var resp WorksResponse
		if err := c.do(ctx, "/works", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		slog.Debug("works page fetched", "page", page, "page_results", len(resp.Results), "total_so_far", len(all))

		if resp.Meta.NextCursor == "" || len(resp.Results) == 0 {
			break
		}
		cursor = resp.Meta.NextCursor
	}
	slog.Info("fetched all works", "author_id", authorID, "total_works", len(all), "pages", page, "duration", time.Since(start))
	return all, nil
}

// SearchRecentWorks finds recent works matching given topic IDs.
func (c *Client) SearchRecentWorks(ctx context.Context, topicIDs []string, since time.Time) ([]Work, error) {
	if len(topicIDs) == 0 {
		slog.Warn("SearchRecentWorks called with no topic IDs")
		return nil, nil
	}

	sinceStr := since.Format("2006-01-02")
	filter := fmt.Sprintf("topics.id:%s,from_publication_date:%s",
		strings.Join(topicIDs, "|"), sinceStr)

	slog.Info("searching recent works", "topic_count", len(topicIDs), "since", sinceStr, "filter", filter)
	start := time.Now()

	var all []Work
	cursor := "*"
	page := 0

	for {
		page++
		params := url.Values{
			"filter":   {filter},
			"per_page": {"200"},
			"cursor":   {cursor},
		}
		var resp WorksResponse
		if err := c.do(ctx, "/works", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		slog.Debug("recent works page fetched",
			"page", page,
			"page_results", len(resp.Results),
			"total_so_far", len(all),
			"meta_count", resp.Meta.Count,
		)

		if resp.Meta.NextCursor == "" || len(resp.Results) == 0 {
			break
		}
		cursor = resp.Meta.NextCursor
	}
	slog.Info("recent works search complete", "total_works", len(all), "pages", page, "duration", time.Since(start))
	return all, nil
}

// SearchRecentWorksByAuthors finds recent works by the given author OpenAlex IDs.
func (c *Client) SearchRecentWorksByAuthors(ctx context.Context, authorIDs []string, since time.Time) ([]Work, error) {
	if len(authorIDs) == 0 {
		slog.Warn("SearchRecentWorksByAuthors called with no author IDs")
		return nil, nil
	}

	sinceStr := since.Format("2006-01-02")
	filter := fmt.Sprintf("authorships.author.id:%s,from_publication_date:%s",
		strings.Join(authorIDs, "|"), sinceStr)

	slog.Info("searching recent works by authors", "author_count", len(authorIDs), "since", sinceStr, "filter", filter)
	start := time.Now()

	var all []Work
	cursor := "*"
	page := 0

	for {
		page++
		params := url.Values{
			"filter":   {filter},
			"per_page": {"200"},
			"cursor":   {cursor},
		}
		var resp WorksResponse
		if err := c.do(ctx, "/works", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		slog.Debug("author works page fetched",
			"page", page,
			"page_results", len(resp.Results),
			"total_so_far", len(all),
			"meta_count", resp.Meta.Count,
		)

		if resp.Meta.NextCursor == "" || len(resp.Results) == 0 {
			break
		}
		cursor = resp.Meta.NextCursor
	}
	slog.Info("author works search complete", "total_works", len(all), "pages", page, "duration", time.Since(start))
	return all, nil
}

// SearchRecentWorksBySubfields finds recent works matching given subfield IDs.
// Uses the filter topics.subfield.id:SF1|SF2,from_publication_date:YYYY-MM-DD.
func (c *Client) SearchRecentWorksBySubfields(ctx context.Context, subfieldIDs []string, since time.Time) ([]Work, error) {
	if len(subfieldIDs) == 0 {
		slog.Warn("SearchRecentWorksBySubfields called with no subfield IDs")
		return nil, nil
	}

	sinceStr := since.Format("2006-01-02")
	filter := fmt.Sprintf("topics.subfield.id:%s,from_publication_date:%s",
		strings.Join(subfieldIDs, "|"), sinceStr)

	slog.Info("searching recent works by subfields", "subfield_count", len(subfieldIDs), "since", sinceStr, "filter", filter)
	start := time.Now()

	var all []Work
	cursor := "*"
	page := 0

	for {
		page++
		params := url.Values{
			"filter":   {filter},
			"per_page": {"200"},
			"cursor":   {cursor},
		}
		var resp WorksResponse
		if err := c.do(ctx, "/works", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		slog.Debug("subfield works page fetched",
			"page", page,
			"page_results", len(resp.Results),
			"total_so_far", len(all),
			"meta_count", resp.Meta.Count,
		)

		if resp.Meta.NextCursor == "" || len(resp.Results) == 0 {
			break
		}
		cursor = resp.Meta.NextCursor
	}
	slog.Info("subfield works search complete", "total_works", len(all), "pages", page, "duration", time.Since(start))
	return all, nil
}

// GetWork fetches a single work by OpenAlex ID.
func (c *Client) GetWork(ctx context.Context, id string) (*Work, error) {
	slog.Debug("fetching work", "id", id)
	var work Work
	if err := c.do(ctx, "/works/"+url.PathEscape(id), nil, &work); err != nil {
		return nil, err
	}
	return &work, nil
}

// GetSources batch-fetches source details by their OpenAlex IDs.
// Uses the filter openalex:S1|S2|S3 syntax.
func (c *Client) GetSources(ctx context.Context, sourceIDs []string) ([]SourceDetail, error) {
	if len(sourceIDs) == 0 {
		return nil, nil
	}

	filter := "openalex:" + strings.Join(sourceIDs, "|")
	slog.Info("fetching sources", "count", len(sourceIDs))
	start := time.Now()

	var all []SourceDetail
	cursor := "*"
	page := 0

	for {
		page++
		params := url.Values{
			"filter":   {filter},
			"per_page": {"200"},
			"cursor":   {cursor},
		}
		var resp SourcesResponse
		if err := c.do(ctx, "/sources", params, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		slog.Debug("sources page fetched", "page", page, "page_results", len(resp.Results), "total_so_far", len(all))

		if resp.Meta.NextCursor == "" || len(resp.Results) == 0 {
			break
		}
		cursor = resp.Meta.NextCursor
	}
	slog.Info("fetched sources", "total", len(all), "pages", page, "duration", time.Since(start))
	return all, nil
}

// GetWorksByIDs batch-fetches works by their OpenAlex IDs.
// Uses the filter openalex:W1|W2|W3 syntax, chunked into batches of 50.
// Only fetches id and authorships fields to minimize response size.
func (c *Client) GetWorksByIDs(ctx context.Context, workIDs []string) ([]Work, error) {
	if len(workIDs) == 0 {
		return nil, nil
	}

	slog.Info("batch-fetching works by IDs", "total_ids", len(workIDs))
	start := time.Now()

	const batchSize = 50
	var all []Work

	for i := 0; i < len(workIDs); i += batchSize {
		end := min(i+batchSize, len(workIDs))
		batch := workIDs[i:end]

		filter := "openalex:" + strings.Join(batch, "|")
		params := url.Values{
			"filter":   {filter},
			"select":   {"id,authorships"},
			"per_page": {"50"},
		}
		var resp WorksResponse
		if err := c.do(ctx, "/works", params, &resp); err != nil {
			return nil, fmt.Errorf("batch fetch works (batch %d): %w", i/batchSize+1, err)
		}
		all = append(all, resp.Results...)
		slog.Debug("works batch fetched", "batch", i/batchSize+1, "batch_results", len(resp.Results), "total_so_far", len(all))
	}

	slog.Info("batch-fetched works by IDs", "total", len(all), "duration", time.Since(start))
	return all, nil
}

// SearchTopics searches for topics by query string.
func (c *Client) SearchTopics(ctx context.Context, query string) ([]TopicSearchResult, error) {
	slog.Debug("searching topics", "query", query)
	params := url.Values{
		"search":   {query},
		"per_page": {"10"},
	}
	var resp TopicsSearchResponse
	if err := c.do(ctx, "/topics", params, &resp); err != nil {
		return nil, err
	}
	slog.Debug("topic search results", "query", query, "count", len(resp.Results))
	return resp.Results, nil
}
