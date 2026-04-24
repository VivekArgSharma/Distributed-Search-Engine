package indexer

import (
	"fmt"
	"testing"
)

func TestSearchOnCrawledIndex(t *testing.T) {
	idx := NewIndexer()

	if err := idx.Load("../index_data"); err != nil {
		t.Skipf("Skipping: could not load index: %v", err)
	}

	fmt.Printf("Loaded %d documents\n\n", idx.DocCount())

	fmt.Println("=== TF-IDF Search: 'concurrency' ===")
	results, dur := idx.Search("concurrency", 5)
	fmt.Printf("Took: %v\n", dur)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	fmt.Println("\n=== TF-IDF Search: 'programming' ===")
	results, dur = idx.Search("programming", 5)
	fmt.Printf("Took: %v\n", dur)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	bm25 := NewBM25(idx)

	fmt.Println("\n=== BM25 Search: 'concurrency' ===")
	bmResults, bmDur := bm25.Search("concurrency", 5)
	fmt.Printf("Took: %v\n", bmDur)
	for _, r := range bmResults {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	fmt.Println("\n=== BM25 Search: 'programming' ===")
	bmResults, bmDur = bm25.Search("programming", 5)
	fmt.Printf("Took: %v\n", bmDur)
	for _, r := range bmResults {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	if len(results) == 0 {
		t.Error("expected search results")
	}
}
