package indexer

import (
	"fmt"
	"testing"

	"search-engine/parser"
)

func TestBM25(t *testing.T) {
	idx := NewIndexer()

	docs := []struct {
		url   string
		title string
		text  string
	}{
		{"https://example.com/1", "Go Programming Language", "Go is a programming language designed at Google. It is concurrent and garbage collected."},
		{"https://example.com/2", "Python Programming", "Python is a high-level programming language. It is interpreted and dynamic."},
		{"https://example.com/3", "JavaScript Web", "JavaScript is a programming language for the web. It runs in browsers."},
		{"https://example.com/4", "Go Concurrency", "Go supports concurrency with goroutines and channels. It is efficient for parallel tasks."},
		{"https://example.com/5", "Python Data Science", "Python is popular for data science and machine learning with many libraries."},
	}

	for _, doc := range docs {
		idx.Index(parser.ParsedPage{
			URL:   doc.url,
			Title: doc.title,
			Text:  doc.text,
		})
	}

	bm25 := NewBM25(idx)

	fmt.Println("\n=== BM25 Search: 'Go programming' ===")
	results, _ := bm25.Search("Go programming", 3)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	fmt.Println("\n=== BM25 vs TF-IDF: 'apple' ===")
	bmResults, _ := bm25.Search("apple", 4)
	tfResults, tfTime := idx.Search("apple", 4)

	fmt.Println("BM25:")
	for _, r := range bmResults {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}
	fmt.Printf("TF-IDF (took %v):\n", tfTime)
	for _, r := range tfResults {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	if len(results) == 0 {
		t.Error("expected results")
	}
}

func TestBM25OnSavedIndex(t *testing.T) {
	idx := NewIndexer()

	if err := idx.Load("../index_data"); err != nil {
		t.Skipf("Skipping: could not load index: %v", err)
	}

	fmt.Printf("\nLoaded %d documents\n\n", idx.DocCount())

	bm25 := NewBM25(idx)

	fmt.Println("=== BM25 Search: 'programming' ===")
	results, _ := bm25.Search("programming", 5)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n  %s\n", r.Score, r.Title, r.URL)
	}

	fmt.Println("\n=== BM25 Search: 'concurrency' ===")
	results, _ = bm25.Search("concurrency", 5)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n  %s\n", r.Score, r.Title, r.URL)
	}

	if len(results) == 0 {
		t.Error("expected search results")
	}
}
