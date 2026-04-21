package indexer

import (
	"bytes"
	"fmt"

	"search-engine/crawler"
	"search-engine/parser"
)

type Engine struct {
	indexer *Indexer
	crawler *crawler.Crawler
}

func NewEngine(idx *Indexer, c *crawler.Crawler) *Engine {
	return &Engine{
		indexer: idx,
		crawler: c,
	}
}

func (e *Engine) CrawlAndIndex(seeds []string) {
	pageChan := make(chan parser.ParsedPage, 100)

	go e.crawler.Start(seeds)

	go func() {
		pages := make(chan crawler.Page, 100)

		go func() {
			for page := range pages {
				reader := bytes.NewReader(page.Body)
				parsed := parser.Parse(reader, page.URL)
				pageChan <- parsed
			}
		}()

	}()

	for parsed := range pageChan {
		fmt.Printf("Indexing: %s\n", parsed.Title)
		e.indexer.Index(parsed)
	}

	fmt.Printf("Indexed %d documents\n", e.indexer.DocCount())
}

func (e *Engine) Search(query string, limit int) []SearchResult {
	return e.indexer.Search(query, limit)
}

func (e *Engine) DocCount() int {
	return e.indexer.DocCount()
}

func (e *Engine) GetDocument(docID string) (*Document, bool) {
	return e.indexer.GetDocument(docID)
}
