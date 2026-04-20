package crawler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"search-engine/parser"
)

type URLJob struct {
	URL   string
	Depth int
}

type Page struct {
	URL   string
	Depth int
	Body  []byte
}

type Crawler struct {
	maxWorkers int
	maxPages   int
	maxDepth   int

	visited map[string]bool
	mu      sync.Mutex

	jobs  chan URLJob
	pages chan Page

	wg sync.WaitGroup

	pageCount int

	rateLimiter <-chan time.Time
}

func NewCrawler(workers, maxPages, maxDepth int, rate time.Duration) *Crawler {
	return &Crawler{
		maxWorkers:  workers,
		maxPages:    maxPages,
		maxDepth:    maxDepth,
		visited:     make(map[string]bool),
		jobs:        make(chan URLJob, 1000),
		pages:       make(chan Page, 1000),
		rateLimiter: time.Tick(rate),
	}
}

func (c *Crawler) Start(seedURLs []string) {

	// start fetch workers
	for i := 0; i < c.maxWorkers; i++ {
		go c.fetchWorker(i)
	}

	// start parser workers
	for i := 0; i < c.maxWorkers; i++ {
		go c.parseWorker(i)
	}

	// seed URLs
	for _, u := range seedURLs {
		c.wg.Add(1)
		c.jobs <- URLJob{URL: u, Depth: 0}
	}

	c.wg.Wait()

	close(c.jobs)
	close(c.pages)

	fmt.Println("Crawling finished")
}

func (c *Crawler) fetchWorker(id int) {
	for job := range c.jobs {

		if !c.shouldVisit(job) {
			c.wg.Done()
			continue
		}

		<-c.rateLimiter // rate limit

		client := http.Client{Timeout: 5 * time.Second}

		req, err := http.NewRequest("GET", job.URL, nil)
		if err != nil {
			c.wg.Done()
			continue
		}

		req.Header.Set("User-Agent", "VivekSearchBot/1.0")

		resp, err := client.Do(req)
		if err != nil {
			c.wg.Done()
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			c.wg.Done()
			continue
		}

		fmt.Printf("[Fetcher %d] %s\n", id, job.URL)

		// send to parser
		select {
		case c.pages <- Page{
			URL:   job.URL,
			Depth: job.Depth,
			Body:  body,
		}:
		default:
			// drop if full
			c.wg.Done()
		}
	}
}

func (c *Crawler) parseWorker(id int) {
	for page := range c.pages {

		reader := bytes.NewReader(page.Body)
		parsed := parser.Parse(reader, page.URL)

		fmt.Printf("[Parser %d] %s\n", id, parsed.Title)

		count := 0
		for _, link := range parsed.Links {

			// limit explosion
			if count >= 100 {
				break
			}
			count++

			if !c.validLink(link) {
				continue
			}

			if c.canAddMore() {
				c.wg.Add(1)

				select {
				case c.jobs <- URLJob{
					URL:   link,
					Depth: page.Depth + 1,
				}:
				default:
					// drop safely
					c.wg.Done()
				}
			}
		}

		c.wg.Done()
	}
}
func (c *Crawler) validLink(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}

	// restrict to English Wikipedia
	if u.Hostname() != "en.wikipedia.org" {
		return false
	}

	// remove fragments
	if strings.Contains(link, "#") {
		return false
	}

	// only article pages
	if !strings.Contains(link, "/wiki/") {
		return false
	}

	//skip special pages
	parts := strings.Split(link, "/wiki/")
	if len(parts) > 1 && strings.Contains(parts[1], ":") {
		return false
	}

	return true
}

func (c *Crawler) shouldVisit(job URLJob) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pageCount >= c.maxPages {
		return false
	}

	if job.Depth > c.maxDepth {
		return false
	}

	if c.visited[job.URL] {
		return false
	}

	c.visited[job.URL] = true
	c.pageCount++
	return true
}

func (c *Crawler) canAddMore() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pageCount < c.maxPages
}
