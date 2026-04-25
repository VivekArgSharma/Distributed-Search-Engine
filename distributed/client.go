package distributed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"search-engine/indexer"
)

type SearchResponse struct {
	Service  string                 `json:"service"`
	Mode     string                 `json:"mode"`
	Query    string                 `json:"query"`
	Took     string                 `json:"took"`
	DocCount int                    `json:"doc_count"`
	ShardIDs []int                  `json:"shard_ids"`
	Results  []indexer.SearchResult `json:"results"`
}

type Client struct {
	httpClient *http.Client
	services   []string
}

func NewClient(services []string) *Client {
	cleaned := make([]string, 0, len(services))
	for _, service := range services {
		trimmed := strings.TrimSpace(service)
		if trimmed != "" {
			cleaned = append(cleaned, strings.TrimRight(trimmed, "/"))
		}
	}

	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		services:   cleaned,
	}
}

func (c *Client) Search(query string, limit int, mode indexer.SearchMode) ([]indexer.SearchResult, time.Duration, error) {
	start := time.Now()
	if len(c.services) == 0 {
		return nil, 0, fmt.Errorf("no shard services configured")
	}

	responses := make([]SearchResponse, len(c.services))
	errors := make([]error, len(c.services))
	var wg sync.WaitGroup

	for i, service := range c.services {
		wg.Add(1)
		go func(i int, service string) {
			defer wg.Done()
			response, err := c.searchService(service, query, limit, mode)
			if err != nil {
				errors[i] = err
				return
			}
			responses[i] = response
		}(i, service)
	}

	wg.Wait()

	merged := make([]indexer.SearchResult, 0, limit*len(c.services))
	for i, response := range responses {
		if errors[i] != nil {
			continue
		}
		merged = append(merged, response.Results...)
	}

	if len(merged) == 0 {
		for _, err := range errors {
			if err != nil {
				return nil, 0, err
			}
		}
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	if len(merged) > limit {
		merged = merged[:limit]
	}

	return merged, time.Since(start), nil
}

func (c *Client) searchService(service, query string, limit int, mode indexer.SearchMode) (SearchResponse, error) {
	endpoint := service + "/search?query=" + url.QueryEscape(query) + "&limit=" + fmt.Sprint(limit) + "&mode=" + string(mode)
	response, err := c.httpClient.Get(endpoint)
	if err != nil {
		return SearchResponse{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return SearchResponse{}, fmt.Errorf("service %s returned status %d", service, response.StatusCode)
	}

	var payload SearchResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return SearchResponse{}, err
	}

	return payload, nil
}
