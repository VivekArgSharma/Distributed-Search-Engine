package crawler

import (
	"fmt"
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

type Crawler struct {
	maxWorkers int
	maxPages   int
	maxDepth   int
	baseDomain string

	visited map[string]bool
	mu      sync.Mutex

	jobs chan URLJob
	wg   sync.WaitGroup

	pageCount int

	rateLimiter <-chan time.Time // global rate limiter
}

func NewCrawler(workers, maxPages, maxDepth int, rate time.Duration, baseDomain string) *Crawler {
	return &Crawler{
		maxWorkers:  workers,
		maxPages:    maxPages,
		maxDepth:    maxDepth,
		baseDomain:  baseDomain,
		visited:     make(map[string]bool),
		jobs:        make(chan URLJob, 200),
		rateLimiter: time.Tick(rate),
	}
}

func (c *Crawler) Start(seedURLs []string) {
	for i := 0; i < c.maxWorkers; i++ {
		go c.worker(i)
	}

	for _, u := range seedURLs {
		c.wg.Add(1)
		c.jobs <- URLJob{URL: u, Depth: 0}
	}

	c.wg.Wait()
	close(c.jobs)

	fmt.Println("Crawling finished")
}

func (c *Crawler) worker(id int) {
	for job := range c.jobs {
		c.process(job, id)
	}
}

func (c *Crawler) process(job URLJob, workerID int) {
	defer c.wg.Done()

	if !c.shouldVisit(job) {
		return
	}

	<-c.rateLimiter // GLOBAL rate limit

	fmt.Printf("[Worker %d] Fetching: %s (Depth %d)\n", workerID, job.URL, job.Depth)

	client := http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest("GET", job.URL, nil)
	if err != nil {
		fmt.Println("Request error:", err)
		return
	}

	req.Header.Set("User-Agent", "VivekSearchBot/1.0 (learning project)")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("[Worker %d] Done: %s (%d)\n", workerID, job.URL, resp.StatusCode)

	parsed := parser.Parse(resp.Body, job.URL)

	// Safe preview
	preview := parsed.Text
	if len(preview) > 100 {
		preview = preview[:100]
	}

	fmt.Println("Title:", parsed.Title)
	fmt.Println("Text preview:", preview)
	fmt.Println("Links found:", len(parsed.Links))

	count := 0
	for _, link := range parsed.Links {

		// limit explosion per page
		if count >= 50 {
			break
		}
		count++

		if !c.validLink(link) {
			continue
		}

		if c.canAddMore() {
			c.wg.Add(1)

			// NON-BLOCKING SEND → prevents deadlock
			select {
			case c.jobs <- URLJob{
				URL:   link,
				Depth: job.Depth + 1,
			}:
			default:
				// queue full → drop safely
				c.wg.Done()
			}
		}
	}
}

func (c *Crawler) validLink(link string) bool {
	// domain restriction (safe version)
	u, err := url.Parse(link)
	if err != nil {
		return false
	}

	if u.Hostname() != "en.wikipedia.org" {
		return false
	}

	// remove fragments
	if strings.Contains(link, "#") {
		return false
	}

	// keep only wiki pages
	if !strings.Contains(link, "/wiki/") {
		return false
	}

	// skip special pages
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
