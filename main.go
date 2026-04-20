package main

import (
	"search-engine/crawler"
	"time"
)

func main() {
	c := crawler.NewCrawler(
		5,  // workers
		50, // max pages
		2,  // depth
		300*time.Millisecond,
	)

	seeds := []string{
		"https://en.wikipedia.org/wiki/Go_(programming_language)",
	}

	c.Start(seeds)
}
