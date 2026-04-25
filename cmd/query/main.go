package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"search-engine/distributed"
	"search-engine/indexer"
)

func main() {
	query := flag.String("query", "programming", "query text")
	limit := flag.Int("limit", 5, "max number of results")
	mode := flag.String("mode", string(indexer.SearchModeBM25), "search mode: tfidf or bm25")
	servicesArg := flag.String("services", defaultServices(), "comma-separated shard service URLs")
	flag.Parse()

	client := distributed.NewClient(strings.Split(*servicesArg, ","))
	results, took, err := client.Search(*query, *limit, indexer.SearchMode(*mode))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Distributed %s search for %q took %v\n", *mode, *query, took)
	for _, result := range results {
		fmt.Printf("[%.2f] %s\n  %s\n\n", result.Score, result.Title, result.URL)
	}
}

func defaultServices() string {
	if services := os.Getenv("SHARD_SERVICES"); services != "" {
		return services
	}
	return "http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084"
}
