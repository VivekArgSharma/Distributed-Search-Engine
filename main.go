package main

import (
	"fmt"
	"time"

	"search-engine/crawler"
	"search-engine/indexer"
	"search-engine/parser"
)

func main() {
	numShards := 16
	idx := indexer.NewShardedIndexer(numShards)
	c := crawler.NewCrawler("VivekSearchBot/1.0", 100*time.Millisecond)

	seeds := []string{
		"https://en.wikipedia.org/wiki/Go_(programming_language)",
		"https://en.wikipedia.org/wiki/Python_(programming_language)",
		"https://en.wikipedia.org/wiki/JavaScript",
		"https://en.wikipedia.org/wiki/Rust_(programming_language)",
		"https://en.wikipedia.org/wiki/C++",
		"https://en.wikipedia.org/wiki/Concurrent_computing",
		"https://en.wikipedia.org/wiki/Functional_programming",
		"https://en.wikipedia.org/wiki/Object-oriented_programming",
		"https://en.wikipedia.org/wiki/Machine_learning",
		"https://en.wikipedia.org/wiki/Artificial_intelligence",
	}

	fmt.Printf("Crawling with %d shards...\n", numShards)

	start := time.Now()
	c.Crawl(seeds, func(p parser.ParsedPage) {
		idx.Index(p)
		fmt.Printf("[%d] %s\n", idx.DocCount(), p.Title)
	}, 80, 2)

	fmt.Printf("\nIndexed %d documents in %v\n", idx.DocCount(), time.Since(start))
	fmt.Printf("Number of shards: %d\n", idx.NumShards())

	fmt.Println("\n=== TF-IDF Search: 'programming' ===")
	results, dur := idx.Search("programming", 5)
	fmt.Printf("Took: %v\n", dur)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	fmt.Println("\n=== BM25 Search: 'programming' ===")
	bm25 := indexer.NewBM25(idx)
	bmResults, bmDur := bm25.Search("programming", 5)
	fmt.Printf("Took: %v\n", bmDur)
	for _, r := range bmResults {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	_ = idx.Save("./index_data")
	fmt.Println("\nIndex saved to ./index_data")
}
