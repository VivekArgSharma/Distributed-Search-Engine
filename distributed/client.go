package distributed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

type HotShardTracker struct {
	mu          sync.RWMutex
	termShards  map[string]map[int]int // term -> shard -> hit count
	shardCounts map[int]int            // shard -> total hits
	queryCount  int64
	minQueries  int
	confidence  float64
}

func NewHotShardTracker(minQueries int, confidence float64) *HotShardTracker {
	return &HotShardTracker{
		termShards:  make(map[string]map[int]int),
		shardCounts: make(map[int]int),
		minQueries:  minQueries,
		confidence:  confidence,
	}
}

func (t *HotShardTracker) tokenize(query string) []string {
	re := regexp.MustCompile(`[a-zA-Z0-9]+`)
	tokens := re.FindAllString(strings.ToLower(query), -1)

	var filtered []string
	for _, token := range tokens {
		if len(token) > 2 {
			filtered = append(filtered, token)
		}
	}
	return filtered
}

func (t *HotShardTracker) Learn(query string, shardIDs []int, resultCount int) {
	if resultCount == 0 {
		return
	}

	atomic.AddInt64(&t.queryCount, 1)

	tokens := t.tokenize(query)
	if len(tokens) == 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, token := range tokens {
		if t.termShards[token] == nil {
			t.termShards[token] = make(map[int]int)
		}
		for _, shardID := range shardIDs {
			t.termShards[token][shardID]++
		}
	}

	for _, shardID := range shardIDs {
		t.shardCounts[shardID]++
	}
}

func (t *HotShardTracker) GetHotShards(query string) []int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if atomic.LoadInt64(&t.queryCount) < int64(t.minQueries) {
		return nil // Not enough data yet
	}

	tokens := t.tokenize(query)
	if len(tokens) == 0 {
		return nil
	}

	// Check which shards contain ALL query tokens
	shardCoverage := make(map[int]int)
	for _, token := range tokens {
		shardHits, ok := t.termShards[token]
		if !ok || len(shardHits) == 0 {
			// Term not seen before - can't be confident
			return nil
		}
		for shardID := range shardHits {
			shardCoverage[shardID]++
		}
	}

	// Shard must have ALL tokens to be confident
	var hotShards []int
	for shardID, coverage := range shardCoverage {
		if coverage >= len(tokens) {
			hotShards = append(hotShards, shardID)
		}
	}

	// If less than confidence% of shards are hot, use them
	totalShards := len(t.shardCounts)
	if len(hotShards) > 0 && float64(len(hotShards))/float64(totalShards) <= t.confidence {
		return hotShards
	}

	return nil // Fall back to all shards
}

func (t *HotShardTracker) Stats() (queryCount int64, termCount int, shardCount int) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return atomic.LoadInt64(&t.queryCount), len(t.termShards), len(t.shardCounts)
}

type Client struct {
	httpClient      *http.Client
	services        []string
	serviceShardIDs map[string][]int // service -> shard IDs
	hotTracker      *HotShardTracker
	enableHotShard  bool
}

func NewClient(services []string, enableHotShard bool, minQueries int, confidence float64) *Client {
	cleaned := make([]string, 0, len(services))
	serviceShardIDs := make(map[string][]int)
	shardPerService := len(services)

	for i, service := range services {
		trimmed := strings.TrimSpace(service)
		if trimmed != "" {
			cleaned = append(cleaned, strings.TrimRight(trimmed, "/"))
			// Map each service to its shard IDs (0-3, 4-7, etc.)
			var shards []int
			start := i * shardPerService
			for j := 0; j < shardPerService; j++ {
				shards = append(shards, start+j)
			}
			serviceShardIDs[trimmed] = shards
		}
	}

	var tracker *HotShardTracker
	if enableHotShard {
		tracker = NewHotShardTracker(minQueries, confidence)
	}

	transport := &http.Transport{
		MaxIdleConnsPerHost: 100,
		MaxConnsPerHost:     100,
		IdleConnTimeout:     30 * time.Second,
	}

	return &Client{
		httpClient:      &http.Client{Timeout: 10 * time.Second, Transport: transport},
		services:        cleaned,
		serviceShardIDs: serviceShardIDs,
		hotTracker:      tracker,
		enableHotShard:  enableHotShard,
	}
}

type SearchResultEx struct {
	Results  []indexer.SearchResult
	Duration time.Duration
	Error    error
	ShardIDs []int
	HotShard bool
}

func (c *Client) SearchEx(query string, limit int, mode indexer.SearchMode) SearchResultEx {
	start := time.Now()
	if len(c.services) == 0 {
		return SearchResultEx{Error: fmt.Errorf("no shard services configured")}
	}

	// Hot shard routing
	var servicesToQuery []string
	var usedHotShards bool
	var hotShardIDs []int

	if c.enableHotShard && c.hotTracker != nil {
		hotShardIDs = c.hotTracker.GetHotShards(query)
		if len(hotShardIDs) > 0 {
			usedHotShards = true
			// Map hot shard IDs to services
			seenService := make(map[string]bool)
			for _, shardID := range hotShardIDs {
				serviceIdx := shardID / 4 // Each service has 4 shards
				if serviceIdx < len(c.services) && !seenService[c.services[serviceIdx]] {
					servicesToQuery = append(servicesToQuery, c.services[serviceIdx])
					seenService[c.services[serviceIdx]] = true
				}
			}
		}
	}

	if servicesToQuery == nil {
		servicesToQuery = c.services
	}

	// Collect all shard IDs that would be queried
	allShardIDs := make([]int, 0, 16)
	for i, svc := range servicesToQuery {
		if shards, ok := c.serviceShardIDs[svc]; ok {
			allShardIDs = append(allShardIDs, shards...)
		} else {
			// Fallback: service i has shards i*4 to i*4+3
			for j := 0; j < 4; j++ {
				allShardIDs = append(allShardIDs, i*4+j)
			}
		}
	}

	responses := make([]SearchResponse, len(servicesToQuery))
	errors := make([]error, len(servicesToQuery))
	var wg sync.WaitGroup

	for i, service := range servicesToQuery {
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

	// Track which shards returned results for learning
	if c.enableHotShard && c.hotTracker != nil && !usedHotShards {
		allShardIDs := make([]int, 0, 16)
		for i := 0; i < 16; i++ {
			allShardIDs = append(allShardIDs, i)
		}

		totalResults := 0
		for _, resp := range responses {
			totalResults += len(resp.Results)
		}

		if totalResults > 0 {
			// Map services to shards and learn
			for i, service := range servicesToQuery {
				if errors[i] == nil && responses[i].Results != nil {
					shardIDs := c.serviceShardIDs[service]
					if len(shardIDs) > 0 {
						c.hotTracker.Learn(query, shardIDs, len(responses[i].Results))
					}
				}
			}
		}
	}

	merged := make([]indexer.SearchResult, 0, limit*len(servicesToQuery))
	for i, response := range responses {
		if errors[i] != nil {
			continue
		}
		merged = append(merged, response.Results...)
	}

	if len(merged) == 0 {
		for _, err := range errors {
			if err != nil {
				return SearchResultEx{Error: err}
			}
		}
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].Score > merged[j].Score
	})

	if len(merged) > limit {
		merged = merged[:limit]
	}

	return SearchResultEx{
		Results:  merged,
		Duration: time.Since(start),
		ShardIDs: allShardIDs,
		HotShard: usedHotShards,
	}
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

func (c *Client) HotShardStats() (queryCount int64, termCount int, shardCount int) {
	if c.hotTracker != nil {
		return c.hotTracker.Stats()
	}
	return 0, 0, 0
}
