package indexer

import (
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	DocID   string  `json:"doc_id"`
	URL     string  `json:"url"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

type SearchMode string

const (
	SearchModeTFIDF SearchMode = "tfidf"
	SearchModeBM25  SearchMode = "bm25"
)

type scoredDoc struct {
	docID string
	score float64
}

type shardMetadata struct {
	TotalShards  int
	LoadedShards []int
	DocCount     int
}

type ShardedIndexer struct {
	totalShards int
	shardIDs    []int
	shards      map[int]*Shard
	tokenizer   *regexp.Regexp
	mu          sync.RWMutex
	docCount    int
}

type Shard struct {
	mu            sync.RWMutex
	documents     map[string]*Document
	invertedIndex map[string][]Posting
	docCount      int
}

func NewShardedIndexer(totalShards int) *ShardedIndexer {
	shardIDs := make([]int, totalShards)
	for i := 0; i < totalShards; i++ {
		shardIDs[i] = i
	}
	return NewShardedIndexerForShards(totalShards, shardIDs)
}

func NewShardedIndexerForShards(totalShards int, shardIDs []int) *ShardedIndexer {
	ids := append([]int(nil), shardIDs...)
	sort.Ints(ids)

	idx := &ShardedIndexer{
		totalShards: totalShards,
		shardIDs:    ids,
		shards:      make(map[int]*Shard, len(ids)),
		tokenizer:   regexp.MustCompile(`[a-zA-Z0-9]+`),
	}

	for _, shardID := range ids {
		idx.shards[shardID] = newShard()
	}

	return idx
}

func ParseShardIDs(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	seen := make(map[int]bool, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		id, err := strconv.Atoi(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid shard id %q: %w", trimmed, err)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no shard ids provided")
	}

	sort.Ints(ids)
	return ids, nil
}

func (idx *ShardedIndexer) ShardIDs() []int {
	return append([]int(nil), idx.shardIDs...)
}

func (idx *ShardedIndexer) TotalShards() int {
	return idx.totalShards
}

func (idx *ShardedIndexer) NumShards() int {
	return len(idx.shardIDs)
}

func (idx *ShardedIndexer) DocCount() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.docCount
}

func (idx *ShardedIndexer) Index(parsed parser.ParsedPage) {
	docID := generateDocID(parsed.URL)
	shardID := idx.getShardIndex(docID)
	shard, ok := idx.shards[shardID]
	if !ok {
		return
	}

	shard.mu.Lock()
	defer shard.mu.Unlock()

	if _, exists := shard.documents[docID]; exists {
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

	shard.documents[docID] = doc
	shard.docCount++

	positions := make(map[string][]int)
	for pos, token := range tokens {
		positions[token] = append(positions[token], pos)
	}

	for term, posList := range positions {
		shard.invertedIndex[term] = append(shard.invertedIndex[term], Posting{
			DocID:     docID,
			TF:        len(posList),
			Positions: posList,
		})
	}

	idx.mu.Lock()
	idx.docCount++
	idx.mu.Unlock()
}

func (idx *ShardedIndexer) Search(query string, limit int) ([]SearchResult, time.Duration) {
	start := time.Now()
	results := idx.searchTFIDF(query, limit)
	return results, time.Since(start)
}

func (idx *ShardedIndexer) SearchBM25(query string, limit int) ([]SearchResult, time.Duration) {
	bm25 := NewBM25(idx)
	return bm25.Search(query, limit)
}

func (idx *ShardedIndexer) searchTFIDF(query string, limit int) []SearchResult {
	queryTokens := idx.tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	idx.mu.RLock()
	totalDocs := idx.docCount
	idx.mu.RUnlock()
	if totalDocs == 0 {
		return nil
	}

	docScores := make(map[string]float64)

	for _, token := range queryTokens {
		for _, shardID := range idx.shardIDs {
			shard := idx.shards[shardID]
			shard.mu.RLock()
			postings := shard.invertedIndex[token]
			if len(postings) == 0 {
				shard.mu.RUnlock()
				continue
			}

			idf := math.Log(float64(totalDocs) / float64(len(postings)))
			for _, posting := range postings {
				tf := 1 + math.Log(float64(posting.TF))
				docScores[posting.DocID] += tf * idf
			}
			shard.mu.RUnlock()
		}
	}

	return idx.buildResults(docScores, queryTokens, limit)
}

type BM25 struct {
	idx *ShardedIndexer
	k1  float64
	b   float64
}

func NewBM25(idx *ShardedIndexer) *BM25 {
	return &BM25{idx: idx, k1: 1.5, b: 0.75}
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

	totalDocs := bm.idx.DocCount()
	if totalDocs == 0 {
		return nil
	}

	avgDL := bm.avgDocLength()
	if avgDL == 0 {
		return nil
	}

	docScores := make(map[string]float64)

	for _, token := range queryTokens {
		for _, shardID := range bm.idx.shardIDs {
			shard := bm.idx.shards[shardID]
			shard.mu.RLock()
			postings := shard.invertedIndex[token]
			if len(postings) == 0 {
				shard.mu.RUnlock()
				continue
			}

			df := float64(len(postings))
			idf := math.Log((float64(totalDocs)-df+0.5)/(df+0.5) + 1)
			for _, posting := range postings {
				doc := shard.documents[posting.DocID]
				if doc == nil {
					continue
				}
				tf := float64(posting.TF)
				dl := float64(len(doc.Tokens))
				tfScore := (tf * (bm.k1 + 1)) / (tf + bm.k1*(1-bm.b+bm.b*dl/avgDL))
				docScores[posting.DocID] += tfScore * idf
			}
			shard.mu.RUnlock()
		}
	}

	return bm.idx.buildResults(docScores, queryTokens, limit)
}

func (bm *BM25) avgDocLength() float64 {
	totalLength := 0
	totalDocs := 0

	for _, shardID := range bm.idx.shardIDs {
		shard := bm.idx.shards[shardID]
		shard.mu.RLock()
		for _, doc := range shard.documents {
			totalLength += len(doc.Tokens)
			totalDocs++
		}
		shard.mu.RUnlock()
	}

	if totalDocs == 0 {
		return 0
	}

	return float64(totalLength) / float64(totalDocs)
}

func (idx *ShardedIndexer) buildResults(docScores map[string]float64, queryTokens []string, limit int) []SearchResult {
	results := make([]scoredDoc, 0, len(docScores))
	for docID, score := range docScores {
		results = append(results, scoredDoc{docID: docID, score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	searchResults := make([]SearchResult, 0, min(limit, len(results)))
	for i := 0; i < len(results) && i < limit; i++ {
		doc := idx.getDoc(results[i].docID)
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

func (idx *ShardedIndexer) getDoc(docID string) *Document {
	shardID := idx.getShardIndex(docID)
	shard := idx.shards[shardID]
	if shard == nil {
		return nil
	}

	shard.mu.RLock()
	defer shard.mu.RUnlock()
	return shard.documents[docID]
}

func (idx *ShardedIndexer) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	for _, shardID := range idx.shardIDs {
		shard := idx.shards[shardID]
		shardDir := filepath.Join(dir, fmt.Sprintf("shard_%d", shardID))
		if err := os.MkdirAll(shardDir, 0o755); err != nil {
			return err
		}

		shard.mu.RLock()
		if err := writeGob(filepath.Join(shardDir, "documents.gob"), shard.documents); err != nil {
			shard.mu.RUnlock()
			return err
		}
		if err := writeGob(filepath.Join(shardDir, "inverted_index.gob"), shard.invertedIndex); err != nil {
			shard.mu.RUnlock()
			return err
		}
		shard.mu.RUnlock()
	}

	meta := shardMetadata{
		TotalShards:  idx.totalShards,
		LoadedShards: idx.ShardIDs(),
		DocCount:     idx.DocCount(),
	}

	return writeGob(filepath.Join(dir, "metadata.gob"), meta)
}

func (idx *ShardedIndexer) Load(dir string) error {
	var meta shardMetadata
	if err := readGob(filepath.Join(dir, "metadata.gob"), &meta); err != nil {
		return err
	}
	if meta.TotalShards != idx.totalShards {
		return fmt.Errorf("total shard mismatch: have %d want %d", meta.TotalShards, idx.totalShards)
	}

	loadedDocs := 0
	for _, shardID := range idx.shardIDs {
		shard := idx.shards[shardID]
		if shard == nil {
			shard = newShard()
			idx.shards[shardID] = shard
		}

		shardDir := filepath.Join(dir, fmt.Sprintf("shard_%d", shardID))
		shard.mu.Lock()
		if err := readGob(filepath.Join(shardDir, "documents.gob"), &shard.documents); err != nil {
			shard.mu.Unlock()
			return err
		}
		if err := readGob(filepath.Join(shardDir, "inverted_index.gob"), &shard.invertedIndex); err != nil {
			shard.mu.Unlock()
			return err
		}
		shard.docCount = len(shard.documents)
		loadedDocs += shard.docCount
		shard.mu.Unlock()
	}

	idx.mu.Lock()
	idx.docCount = loadedDocs
	idx.mu.Unlock()
	return nil
}

func newShard() *Shard {
	return &Shard{
		documents:     make(map[string]*Document),
		invertedIndex: make(map[string][]Posting),
	}
}

func writeGob(path string, value any) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return gob.NewEncoder(file).Encode(value)
}

func readGob(path string, target any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return gob.NewDecoder(file).Decode(target)
}

func generateDocID(url string) string {
	return strings.ToLower(strings.TrimPrefix(url, "https://"))
}

func (idx *ShardedIndexer) getShardIndex(docID string) int {
	hash := 0
	for i, c := range docID {
		hash = hash*31 + int(c)*(i+1)
	}
	return hash % idx.totalShards
}

func (idx *ShardedIndexer) tokenize(text string) []string {
	text = strings.ToLower(text)
	tokens := idx.tokenizer.FindAllString(text, -1)
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if len(token) > 2 && !isStopWord(token) {
			filtered = append(filtered, token)
		}
	}
	return filtered
}

func isStopWord(word string) bool {
	switch word {
	case "the", "is", "at", "which", "on", "and", "a", "an", "in", "to", "were", "was", "it", "for", "of", "or", "as", "by", "this", "with", "be", "are", "from", "has", "have", "had", "but", "not", "that", "they":
		return true
	default:
		return false
	}
}

func createSnippet(text string, queryTokens []string) string {
	words := strings.Fields(strings.ToLower(text))
	if len(words) == 0 {
		return ""
	}

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

	end := min(bestStart+50, len(words))
	snippet := strings.Join(words[bestStart:end], " ")
	if len(snippet) > 200 {
		return snippet[:200] + "..."
	}
	return snippet
}
