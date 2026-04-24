package indexer

import (
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

type SearchResult struct {
	DocID   string
	URL     string
	Title   string
	Score   float64
	Snippet string
}

type ShardedIndexer struct {
	shards    map[int]*Shard
	numShards int32
	docCount  int32
	indexMu   sync.Mutex
}

type Shard struct {
	mu            sync.RWMutex
	documents     map[string]*Document
	invertedIndex map[string][]Posting
	docCount      int
}

var tokenizer = regexp.MustCompile(`[a-zA-Z0-9]+`)

func NewShardedIndexer(numShards int) *ShardedIndexer {
	si := &ShardedIndexer{
		shards:    make(map[int]*Shard),
		numShards: int32(numShards),
	}

	for i := 0; i < numShards; i++ {
		si.shards[i] = &Shard{
			documents:     make(map[string]*Document),
			invertedIndex: make(map[string][]Posting),
		}
	}

	return si
}

func (si *ShardedIndexer) Index(parsed parser.ParsedPage) {
	docID := generateDocID(parsed.URL)
	shardIdx := si.getShardIndex(docID)

	shard := si.shards[shardIdx]
	if shard == nil {
		shard = &Shard{
			documents:     make(map[string]*Document),
			invertedIndex: make(map[string][]Posting),
		}
		si.shards[shardIdx] = shard
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, exists := shard.documents[docID]; exists {
		return
	}

	tokens := si.tokenize(parsed.Text)

	doc := &Document{
		ID:     docID,
		URL:    parsed.URL,
		Title:  parsed.Title,
		Text:   parsed.Text,
		Tokens: tokens,
	}

	shard.documents[docID] = doc
	shard.docCount++
	atomic.AddInt32(&si.docCount, 1)

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
		shard.invertedIndex[term] = append(shard.invertedIndex[term], posting)
	}
}

func (si *ShardedIndexer) getShardIndex(docID string) int {
	hash := 0
	for i, c := range docID {
		hash = hash*31 + int(c)*i
	}
	return hash % int(si.numShards)
}

func (si *ShardedIndexer) tokenize(text string) []string {
	text = strings.ToLower(text)
	tokens := tokenizer.FindAllString(text, -1)

	var filtered []string
	for _, token := range tokens {
		if len(token) > 2 && !isStopWord(token) {
			filtered = append(filtered, token)
		}
	}
	return filtered
}

func (si *ShardedIndexer) Search(query string, limit int) ([]SearchResult, time.Duration) {
	start := time.Now()
	results := si.search(query, limit)
	return results, time.Since(start)
}

func (si *ShardedIndexer) search(query string, limit int) []SearchResult {
	queryTokens := si.tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	docScores := make(map[string]float64)

	for _, token := range queryTokens {
		for shardIdx := 0; shardIdx < int(si.numShards); shardIdx++ {
			shard := si.shards[shardIdx]
			shard.mu.RLock()
			postings, ok := shard.invertedIndex[token]
			if !ok {
				shard.mu.RUnlock()
				continue
			}

			df := float64(len(postings))
			N := float64(atomic.LoadInt32(&si.docCount))
			idf := math.Log(N / df)

			for _, posting := range postings {
				tf := 1 + math.Log(float64(posting.TF))
				score := tf * idf
				docScores[posting.DocID] += score
			}
			shard.mu.RUnlock()
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
		doc := si.getDoc(results[i].docID)
		if doc == nil {
			continue
		}
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

type BM25 struct {
	idx *ShardedIndexer
	k1  float64
	b   float64
}

func NewBM25(idx *ShardedIndexer) *BM25 {
	return &BM25{
		idx: idx,
		k1:  1.5,
		b:   0.75,
	}
}

func (bm *BM25) Search(query string, limit int) ([]SearchResult, time.Duration) {
	start := time.Now()
	results := bm.search(query, limit)
	return results, time.Since(start)
}

func (bm *BM25) search(query string, limit int) []SearchResult {
	queryTokens := bm.idx.tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	N := float64(bm.idx.DocCount())
	if N == 0 {
		return nil
	}

	avgDL := bm.avgDocLength()
	docScores := make(map[string]float64)

	for _, token := range queryTokens {
		for shardIdx := 0; shardIdx < bm.idx.NumShards(); shardIdx++ {
			shard := bm.idx.shards[shardIdx]
			shard.mu.RLock()
			postings, ok := shard.invertedIndex[token]
			if !ok {
				shard.mu.RUnlock()
				continue
			}

			df := float64(len(postings))
			idf := math.Log((N - df + 0.5) / (df + 0.5))

			for _, posting := range postings {
				docID := posting.DocID
				tf := float64(posting.TF)
				dl := float64(len(bm.idx.shards[shardIdx].documents[docID].Tokens))

				tfScore := (tf * (bm.k1 + 1)) / (tf + bm.k1*(1-bm.b+bm.b*dl/avgDL))
				docScores[docID] += tfScore * idf
			}
			shard.mu.RUnlock()
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
		doc := bm.idx.getDoc(results[i].docID)
		if doc == nil {
			continue
		}
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

func (bm *BM25) avgDocLength() float64 {
	if bm.idx.DocCount() == 0 {
		return 0
	}
	total := 0
	for shardIdx := 0; shardIdx < bm.idx.NumShards(); shardIdx++ {
		for _, doc := range bm.idx.shards[shardIdx].documents {
			total += len(doc.Tokens)
		}
	}
	return float64(total) / float64(bm.idx.DocCount())
}

func (si *ShardedIndexer) getDoc(docID string) *Document {
	shardIdx := si.getShardIndex(docID)
	shard := si.shards[shardIdx]
	shard.mu.RLock()
	defer shard.mu.RUnlock()
	return shard.documents[docID]
}

func (si *ShardedIndexer) DocCount() int {
	return int(atomic.LoadInt32(&si.docCount))
}

func (si *ShardedIndexer) NumShards() int {
	return int(si.numShards)
}

func (si *ShardedIndexer) Save(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	for shardIdx := 0; shardIdx < int(si.numShards); shardIdx++ {
		shardDir := filepath.Join(dir, fmt.Sprintf("shard_%d", shardIdx))
		if err := os.MkdirAll(shardDir, 0755); err != nil {
			return err
		}

		shard := si.shards[shardIdx]
		shard.mu.Lock()

		docsFile, err := os.Create(filepath.Join(shardDir, "documents.gob"))
		if err != nil {
			return err
		}
		enc := gob.NewEncoder(docsFile)
		if err := enc.Encode(shard.documents); err != nil {
			return err
		}
		docsFile.Close()

		indexFile, err := os.Create(filepath.Join(shardDir, "inverted_index.gob"))
		if err != nil {
			return err
		}
		enc = gob.NewEncoder(indexFile)
		if err := enc.Encode(shard.invertedIndex); err != nil {
			return err
		}
		indexFile.Close()

		shard.mu.Unlock()
	}

	metadataFile, err := os.Create(filepath.Join(dir, "metadata.gob"))
	if err != nil {
		return err
	}
	enc := gob.NewEncoder(metadataFile)
	if err := enc.Encode(struct {
		NumShards int
		DocCount  int
	}{int(si.numShards), int(si.docCount)}); err != nil {
		return err
	}
	metadataFile.Close()

	return nil
}

func (si *ShardedIndexer) Load(dir string) error {
	metadataFile, err := os.Open(filepath.Join(dir, "metadata.gob"))
	if err != nil {
		return err
	}
	dec := gob.NewDecoder(metadataFile)
	var meta struct {
		NumShards int
		DocCount  int
	}
	if err := dec.Decode(&meta); err != nil {
		return err
	}
	metadataFile.Close()

	if meta.NumShards != int(si.numShards) {
		return fmt.Errorf("shard count mismatch: have %d, want %d", meta.NumShards, si.numShards)
	}

	atomic.StoreInt32(&si.docCount, int32(meta.DocCount))

	for shardIdx := 0; shardIdx < int(si.numShards); shardIdx++ {
		shardDir := filepath.Join(dir, fmt.Sprintf("shard_%d", shardIdx))

		docsFile, err := os.Open(filepath.Join(shardDir, "documents.gob"))
		if err != nil {
			return err
		}
		dec = gob.NewDecoder(docsFile)
		if err := dec.Decode(&si.shards[shardIdx].documents); err != nil {
			return err
		}
		docsFile.Close()

		indexFile, err := os.Open(filepath.Join(shardDir, "inverted_index.gob"))
		if err != nil {
			return err
		}
		dec = gob.NewDecoder(indexFile)
		if err := dec.Decode(&si.shards[shardIdx].invertedIndex); err != nil {
			return err
		}
		indexFile.Close()

		si.shards[shardIdx].docCount = len(si.shards[shardIdx].documents)
	}

	return nil
}

func generateDocID(url string) string {
	hash := 0
	for i, c := range url {
		hash = hash*31 + int(c)*i
	}
	return strings.ToLower(strings.ReplaceAll(url, "https://", ""))
}

func isStopWord(word string) bool {
	return word == "the" || word == "is" || word == "at" || word == "which" || word == "on" ||
		word == "and" || word == "a" || word == "an" || word == "in" || word == "to" ||
		word == "were" || word == "was" || word == "it" || word == "for" || word == "of" ||
		word == "or" || word == "as" || word == "by" || word == "this" || word == "with" ||
		word == "be" || word == "are" || word == "from" || word == "has" || word == "have" ||
		word == "had" || word == "but" || word == "not" || word == "that" || word == "they"
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
