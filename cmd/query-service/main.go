package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"search-engine/distributed"
	"search-engine/indexer"
)

type response struct {
	Query   string                 `json:"query"`
	Mode    string                 `json:"mode"`
	Took    string                 `json:"took"`
	Results []indexer.SearchResult `json:"results"`
}

func main() {
	port := envOrDefault("PORT", "8090")
	services := strings.Split(envOrDefault("SHARD_SERVICES", "http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084"), ",")
	client := distributed.NewClient(services)

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"service":   "query-service",
			"upstreams": services,
		})
	})

	http.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
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

		results, took, err := client.Search(query, limit, mode)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, response{
			Query:   query,
			Mode:    string(mode),
			Took:    took.String(),
			Results: results,
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
