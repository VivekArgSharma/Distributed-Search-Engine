package crawler

import (
	"bytes"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"search-engine/parser"
)

type Crawler struct {
	client    *http.Client
	userAgent string
	rateLimit time.Duration

	mu      sync.Mutex
	visited map[string]bool
	checked int
}

func NewCrawler(userAgent string, rateLimit time.Duration) *Crawler {
	return &Crawler{
		client:    &http.Client{Timeout: 15 * time.Second},
		userAgent: userAgent,
		rateLimit: rateLimit,
		visited:   make(map[string]bool),
	}
}

func (c *Crawler) Fetch(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *Crawler) FetchAndParse(url string) parser.ParsedPage {
	body, err := c.Fetch(url)
	if err != nil {
		return parser.ParsedPage{}
	}
	return parser.Parse(bytes.NewReader(body), url)
}

func (c *Crawler) ExtractLinks(body []byte) []string {
	re := regexp.MustCompile(`href="(/wiki/[^"#:]+)"`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	var links []string
	for _, m := range matches {
		if len(m) > 1 && !strings.Contains(m[1], ":") {
			links = append(links, "https://en.wikipedia.org"+m[1])
		}
	}
	return links
}

func (c *Crawler) Crawl(urls []string, onPage func(parser.ParsedPage), maxPages, depth int) int {
	var wg sync.WaitGroup

	var crawl func(u string, d int)
	crawl = func(u string, d int) {
		c.mu.Lock()
		if c.checked >= maxPages || c.visited[u] {
			c.mu.Unlock()
			return
		}
		c.visited[u] = true
		c.checked++
		c.mu.Unlock()

		body, err := c.Fetch(u)
		if err != nil {
			return
		}

		parsed := parser.Parse(bytes.NewReader(body), u)
		onPage(parsed)

		if d > 0 {
			links := c.ExtractLinks(body)
			for _, link := range links {
				if c.checked < maxPages {
					time.Sleep(c.rateLimit)
					wg.Add(1)
					go func(l string) {
						defer wg.Done()
						crawl(l, d-1)
					}(link)
				}
			}
		}
	}

	for _, url := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			crawl(u, depth)
		}(url)
	}

	wg.Wait()
	return c.checked
}
