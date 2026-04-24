package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"search-engine/indexer"
	"search-engine/parser"
)

var (
	visited  = make(map[string]bool)
	mu       sync.Mutex
	linkChan = make(chan string, 1000)
)

func fetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "VivekSearchBot/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func crawl(idx *indexer.Indexer, maxPages, depth int) {
	count := 0

	for url := range linkChan {
		mu.Lock()
		if visited[url] || count >= maxPages {
			mu.Unlock()
			if count >= maxPages {
				break
			}
			continue
		}
		visited[url] = true
		count++
		mu.Unlock()

		body, err := fetch(url)
		if err != nil {
			continue
		}

		parsed := parser.Parse(bytes.NewReader(body), url)
		idx.Index(parsed)
		fmt.Printf("[%d] %s\n", idx.DocCount(), parsed.Title)

		if depth > 0 {
			for _, link := range extractLinks(string(body)) {
				select {
				case linkChan <- link:
				default:
				}
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
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

func main() {
	idx := indexer.NewIndexer()

	linkChan <- "https://en.wikipedia.org/wiki/Go_(programming_language)"
	linkChan <- "https://en.wikipedia.org/wiki/Python_(programming_language)"

	start := time.Now()

	for i := 0; i < 5; i++ {
		go crawl(idx, 20, 2)
	}

	time.Sleep(5 * time.Second)
	close(linkChan)

	fmt.Printf("\nIndexed %d documents in %v\n", idx.DocCount(), time.Since(start))

	results, dur := idx.Search("programming", 5)
	fmt.Printf("\n=== Search: 'programming' (took %v) ===\n", dur)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n  %s\n\n", r.Score, r.Title, r.URL)
	}

	results, dur = idx.Search("concurrency", 5)
	fmt.Printf("=== Search: 'concurrency' (took %v) ===\n", dur)
	for _, r := range results {
		fmt.Printf("[%.2f] %s\n  %s\n\n", r.Score, r.Title, r.URL)
	}

	_ = idx.Save("./index_data")
	fmt.Println("Index saved to ./index_data")
}
