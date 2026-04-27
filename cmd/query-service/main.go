package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"search-engine/cache"
	"search-engine/distributed"
	"search-engine/indexer"
)

type response struct {
	Query    string                 `json:"query"`
	Mode     string                 `json:"mode"`
	Took     string                 `json:"took"`
	From     string                 `json:"from"`
	Shards   []int                  `json:"shards,omitempty"`
	HotShard bool                   `json:"hot_shard,omitempty"`
	Results  []indexer.SearchResult `json:"results"`
}

func main() {
	port := envOrDefault("PORT", "8090")
	services := strings.Split(envOrDefault("SHARD_SERVICES", "http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084"), ",")

	enableHotShard := envOrDefault("ENABLE_HOT_SHARD", "true") == "true"
	minQueries := 20
	confidence := 0.5

	client := distributed.NewClient(services, enableHotShard, minQueries, confidence)

	var queryCache *cache.Cache
	redisAddr := envOrDefault("REDIS_ADDR", "localhost:6379")
	redisTTL := envOrDefault("REDIS_TTL", "5m")

	if redisAddr != "" {
		ttl, err := time.ParseDuration(redisTTL)
		if err != nil {
			log.Printf("invalid REDIS_TTL %s, using default 5m", redisTTL)
			ttl = 5 * time.Minute
		}
		queryCache = cache.NewCache(redisAddr, ttl)
		if err := queryCache.Ping(context.Background()); err != nil {
			log.Printf("redis not available, caching disabled: %v", err)
			queryCache = nil
		} else {
			log.Printf("redis cache enabled at %s with TTL %v", redisAddr, ttl)
		}
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		status := map[string]any{
			"service":   "query-service",
			"upstreams": services,
		}
		if queryCache != nil {
			status["cache"] = "enabled"
		} else {
			status["cache"] = "disabled"
		}
		if enableHotShard {
			qc, tc, sc := client.HotShardStats()
			status["hot_shard"] = map[string]any{
				"enabled": true,
				"queries": qc,
				"terms":   tc,
				"shards":  sc,
			}
		}
		writeJSON(w, http.StatusOK, status)
	})

	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		if query == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
			return
		}

		limit := 5
		if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
			parsedLimit, err := strconv.Atoi(rawLimit)
			if err != nil || parsedLimit <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
				return
			}
			limit = parsedLimit
		}

		mode := indexer.SearchMode(r.URL.Query().Get("mode"))
		if mode == "" {
			mode = indexer.SearchModeBM25
		}

		if queryCache != nil {
			cached, err := queryCache.Get(ctx, query, mode, limit)
			if err == nil && cached != nil {
				writeJSON(w, http.StatusOK, response{
					Query:   query,
					Mode:    string(mode),
					Took:    cached.Took,
					From:    "cache",
					Results: cached.Results,
				})
				return
			}
		}

		result := client.SearchEx(query, limit, mode)
		if result.Error != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": result.Error.Error()})
			return
		}

		if queryCache != nil {
			go func() {
				_ = queryCache.Set(context.Background(), query, mode, limit, &cache.CacheResult{
					Results: result.Results,
					Took:    result.Duration.String(),
				})
			}()
		}

		writeJSON(w, http.StatusOK, response{
			Query:    query,
			Mode:     string(mode),
			Took:     result.Duration.String(),
			From:     "search",
			Shards:   result.ShardIDs,
			HotShard: result.HotShard,
			Results:  result.Results,
		})
	})

	log.Printf("query-service listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
