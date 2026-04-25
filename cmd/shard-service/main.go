package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"

	"search-engine/distributed"
	"search-engine/indexer"
)

func main() {
	totalShards := mustEnvInt("TOTAL_SHARDS", 16)
	shardIDs := mustShardIDs(os.Getenv("SHARD_IDS"))
	port := mustEnv("PORT", "8080")
	indexDir := mustEnv("INDEX_DIR", "./index_data")
	serviceName := mustEnv("SERVICE_NAME", "shard-service")

	idx := indexer.NewShardedIndexerForShards(totalShards, shardIDs)
	if err := idx.Load(indexDir); err != nil {
		log.Fatalf("load index: %v", err)
	}

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service":      serviceName,
			"total_shards": totalShards,
			"shard_ids":    idx.ShardIDs(),
			"doc_count":    idx.DocCount(),
		})
	})

	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
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
			mode = indexer.SearchModeTFIDF
		}

		var (
			results []indexer.SearchResult
			took    string
		)

		switch mode {
		case indexer.SearchModeTFIDF:
			searchResults, duration := idx.Search(query, limit)
			results = searchResults
			took = duration.String()
		case indexer.SearchModeBM25:
			searchResults, duration := idx.SearchBM25(query, limit)
			results = searchResults
			took = duration.String()
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be tfidf or bm25"})
			return
		}

		payload := distributed.SearchResponse{
			Service:  serviceName,
			Mode:     string(mode),
			Query:    query,
			Took:     took,
			DocCount: idx.DocCount(),
			ShardIDs: idx.ShardIDs(),
			Results:  results,
		}

		writeJSON(w, http.StatusOK, payload)
	})

	log.Printf("%s serving shards %v on :%s", serviceName, idx.ShardIDs(), port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func mustEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func mustEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalf("invalid %s: %v", key, err)
	}
	return parsed
}

func mustShardIDs(raw string) []int {
	if raw == "" {
		log.Fatal("SHARD_IDS is required")
	}
	ids, err := indexer.ParseShardIDs(raw)
	if err != nil {
		log.Fatalf("parse shard ids: %v", err)
	}
	return ids
}
