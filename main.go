package main

import (
	"search-engine/crawler"
	"time"
)

func main() {
	c := crawler.NewCrawler(
		5,                    // workers
		100,                  // max pages
		2,                    // max depth
		500*time.Millisecond, // rate limit
		"wikipedia.org",      // domain restriction
	)

	seeds := []string{
		"https://en.wikipedia.org/wiki/Go_(programming_language)",
	}

	c.Start(seeds)
}
