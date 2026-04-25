package main

import (
	"fmt"
	"time"

	"search-engine/crawler"
	"search-engine/indexer"
	"search-engine/parser"
)

func main() {
	const totalShards = 16

	idx := indexer.NewShardedIndexer(totalShards)
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

	start := time.Now()
	count := c.Crawl(seeds, func(page parser.ParsedPage) {
		if page.URL == "" {
			return
		}
		idx.Index(page)
		fmt.Printf("[%d] %s\n", idx.DocCount(), page.Title)
	}, 80, 2)

	if err := idx.Save("./index_data"); err != nil {
		panic(err)
	}

	fmt.Printf("\nCrawled %d pages\n", count)
	fmt.Printf("Indexed %d documents in %v\n", idx.DocCount(), time.Since(start))
	fmt.Printf("Saved %d total shards to ./index_data\n", idx.TotalShards())

	results, dur := idx.Search("programming", 5)
	fmt.Printf("\nTF-IDF 'programming' took %v\n", dur)
	for _, result := range results {
		fmt.Printf("[%.2f] %s\n", result.Score, result.Title)
	}

	bm25Results, bm25Dur := idx.SearchBM25("programming", 5)
	fmt.Printf("\nBM25 'programming' took %v\n", bm25Dur)
	for _, result := range bm25Results {
		fmt.Printf("[%.2f] %s\n", result.Score, result.Title)
	}
}
