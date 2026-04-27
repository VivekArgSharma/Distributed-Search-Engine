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
	hotShard := flag.Bool("hot-shard", false, "enable hot shard routing")
	flag.Parse()

	client := distributed.NewClient(strings.Split(*servicesArg, ","), *hotShard, 20, 0.5)
	result := client.SearchEx(*query, *limit, indexer.SearchMode(*mode))
	if result.Error != nil {
		log.Fatal(result.Error)
	}

	fmt.Printf("Distributed %s search for %q took %v (shards: %v, hot: %v)\n", *mode, *query, result.Duration, result.ShardIDs, result.HotShard)
	for _, r := range result.Results {
		fmt.Printf("[%.2f] %s\n  %s\n\n", r.Score, r.Title, r.URL)
	}
}

func defaultServices() string {
	if services := os.Getenv("SHARD_SERVICES"); services != "" {
		return services
	}
	return "http://localhost:8081,http://localhost:8082,http://localhost:8083,http://localhost:8084"
}
