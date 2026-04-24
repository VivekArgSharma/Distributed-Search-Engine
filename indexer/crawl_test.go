package indexer

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"search-engine/parser"
)

func fetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "SearchBotTest/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func extractLinks(body string) []string {
	re := regexp.MustCompile(`href="(/wiki/[^"#:]+)"`)
	matches := re.FindAllStringSubmatch(body, -1)

	var links []string
	for _, m := range matches {
		if len(m) > 1 && !strings.Contains(m[1], ":") {
			links = append(links, "https://en.wikipedia.org"+m[1])
		}
	}
	return links
}

func TestCrawlAndSearch(t *testing.T) {
	idx := NewIndexer()

	urls := []string{
		"https://en.wikipedia.org/wiki/Go_(programming_language)",
		"https://en.wikipedia.org/wiki/Python_(programming_language)",
		"https://en.wikipedia.org/wiki/JavaScript",
		"https://en.wikipedia.org/wiki/Concurrent_computing",
		"https://en.wikipedia.org/wiki/Software_engineering",
	}

	for _, url := range urls {
		body, err := fetch(url)
		if err != nil {
			t.Logf("Failed to fetch %s: %v", url, err)
			continue
		}

		parsed := parser.Parse(bytes.NewReader(body), url)
		idx.Index(parsed)
		fmt.Printf("[%d] %s\n", idx.DocCount(), parsed.Title)
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("\nIndexed %d documents\n\n", idx.DocCount())

	fmt.Println("=== TF-IDF Search: 'concurrency' ===")
	results, dur := idx.Search("concurrency", 5)
	fmt.Printf("Took: %v\n", dur)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	fmt.Println("\n=== TF-IDF Search: 'programming language' ===")
	results, dur = idx.Search("programming language", 5)
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

	fmt.Println("\n=== BM25 Search: 'programming language' ===")
	bmResults, bmDur = bm25.Search("programming language", 5)
	fmt.Printf("Took: %v\n", bmDur)
	for _, r := range bmResults {
		fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
	}

	if len(results) == 0 {
		t.Error("expected search results")
	}
}
