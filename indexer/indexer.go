package indexer

import (
	"encoding/gob"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"search-engine/parser"
)

type Document struct {
	ID     string
	URL    string
	Title  string
	Text   string
	Tokens []string
}

type Posting struct {
	DocID     string
	TF        int
	Positions []int
}

type Indexer struct {
	documents     map[string]*Document
	invertedIndex map[string][]Posting
	mu            sync.RWMutex

	docCount int

	tokenizer *regexp.Regexp
}

func NewIndexer() *Indexer {
	return &Indexer{
		documents:     make(map[string]*Document),
		invertedIndex: make(map[string][]Posting),
		tokenizer:     regexp.MustCompile(`[a-zA-Z0-9]+`),
	}
}

func (idx *Indexer) Index(parsed parser.ParsedPage) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	docID := generateDocID(parsed.URL)

	if _, exists := idx.documents[docID]; exists {
		return
	}

	tokens := idx.tokenize(parsed.Text)

	doc := &Document{
		ID:     docID,
		URL:    parsed.URL,
		Title:  parsed.Title,
		Text:   parsed.Text,
		Tokens: tokens,
	}

	idx.documents[docID] = doc
	idx.docCount++

	positions := make(map[string][]int)
	for pos, token := range tokens {
		positions[token] = append(positions[token], pos)
	}

	for term, posList := range positions {
		posting := Posting{
			DocID:     docID,
			TF:        len(posList),
			Positions: posList,
		}
		idx.invertedIndex[term] = append(idx.invertedIndex[term], posting)
	}
}

func (idx *Indexer) tokenize(text string) []string {
	text = strings.ToLower(text)
	tokens := idx.tokenizer.FindAllString(text, -1)

	var filtered []string
	for _, token := range tokens {
		if len(token) > 2 && !isStopWord(token) {
			filtered = append(filtered, token)
		}
	}
	return filtered
}

func (idx *Indexer) Search(query string, limit int) []SearchResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	queryTokens := idx.tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	docScores := make(map[string]float64)

	for _, token := range queryTokens {
		postings, ok := idx.invertedIndex[token]
		if !ok {
			continue
		}

		idf := math.Log(float64(idx.docCount) / float64(len(postings)))

		for _, posting := range postings {
			tf := 1 + math.Log(float64(posting.TF))
			score := tf * idf
			docScores[posting.DocID] += score
		}
	}

	type scoredDoc struct {
		docID string
		score float64
	}

	var results []scoredDoc
	for docID, score := range docScores {
		results = append(results, scoredDoc{docID, score})
	}

	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	var searchResults []SearchResult
	for i := 0; i < len(results) && i < limit; i++ {
		doc := idx.documents[results[i].docID]
		searchResults = append(searchResults, SearchResult{
			DocID:   doc.ID,
			URL:     doc.URL,
			Title:   doc.Title,
			Score:   results[i].score,
			Snippet: createSnippet(doc.Text, queryTokens),
		})
	}

	return searchResults
}

type SearchResult struct {
	DocID   string
	URL     string
	Title   string
	Score   float64
	Snippet string
}

func (idx *Indexer) DocCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docCount
}

func (idx *Indexer) GetDocument(docID string) (*Document, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	doc, ok := idx.documents[docID]
	return doc, ok
}

func (idx *Indexer) Save(dir string) error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	docsFile, err := os.Create(filepath.Join(dir, "documents.gob"))
	if err != nil {
		return err
	}
	defer docsFile.Close()

	enc := gob.NewEncoder(docsFile)
	if err := enc.Encode(idx.documents); err != nil {
		return err
	}

	indexFile, err := os.Create(filepath.Join(dir, "inverted_index.gob"))
	if err != nil {
		return err
	}
	defer indexFile.Close()

	enc = gob.NewEncoder(indexFile)
	if err := enc.Encode(idx.invertedIndex); err != nil {
		return err
	}

	return nil
}

func (idx *Indexer) Load(dir string) error {
	docsFile, err := os.Open(filepath.Join(dir, "documents.gob"))
	if err != nil {
		return err
	}
	defer docsFile.Close()

	dec := gob.NewDecoder(docsFile)
	if err := dec.Decode(&idx.documents); err != nil {
		return err
	}

	indexFile, err := os.Open(filepath.Join(dir, "inverted_index.gob"))
	if err != nil {
		return err
	}
	defer indexFile.Close()

	dec = gob.NewDecoder(indexFile)
	if err := dec.Decode(&idx.invertedIndex); err != nil {
		return err
	}

	idx.docCount = len(idx.documents)
	return nil
}

func generateDocID(url string) string {
	hash := 0
	for i, c := range url {
		hash = hash*31 + int(c)*i
	}
	return strings.ToLower(strings.ReplaceAll(url, "https://", ""))
}

var stopWords = map[string]bool{
	"the": true, "is": true, "at": true, "which": true, "on": true,
	"and": true, "a": true, "an": true, "in": true, "to": true,
	"were": true, "was": true, "it": true, "for": true, "of": true,
	"or": true, "as": true, "by": true, "this": true, "with": true,
	"be": true, "are": true, "from": true, "has": true, "have": true,
	"had": true, "but": true, "not": true, "that": true, "they": true,
}

func isStopWord(word string) bool {
	return stopWords[word]
}

func createSnippet(text string, queryTokens []string) string {
	text = strings.ToLower(text)
	words := strings.Fields(text)

	maxScore := 0
	bestStart := 0

	for i := 0; i < len(words); i++ {
		score := 0
		for j := i; j < len(words) && j < i+50; j++ {
			for _, token := range queryTokens {
				if words[j] == token {
					score++
				}
			}
		}
		if score > maxScore {
			maxScore = score
			bestStart = i
		}
	}

	start := bestStart
	end := start + 50
	if start > len(words)-1 {
		start = 0
		end = min(50, len(words))
	}
	if end > len(words) {
		end = len(words)
	}

	snippet := strings.Join(words[start:end], " ")
	if len(snippet) > 200 {
		snippet = snippet[:200] + "..."
	}

	return snippet
}
