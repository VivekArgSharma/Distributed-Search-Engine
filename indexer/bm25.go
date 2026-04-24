package indexer

import (
	"math"
	"strings"
	"time"
)

type BM25 struct {
	k1  float64
	b   float64
	idx *Indexer
}

func NewBM25(idx *Indexer) *BM25 {
	return &BM25{
		k1:  1.5,
		b:   0.75,
		idx: idx,
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

	N := float64(bm.idx.docCount)
	if N == 0 {
		return nil
	}

	avgDL := bm.avgDocLength()
	docScores := make(map[string]float64)

	for _, token := range queryTokens {
		postings, ok := bm.idx.invertedIndex[token]
		if !ok {
			continue
		}

		df := float64(len(postings))
		idf := math.Log((N - df + 0.5) / (df + 0.5))

		for _, posting := range postings {
			docID := posting.DocID
			tf := float64(posting.TF)
			dl := float64(len(bm.idx.documents[docID].Tokens))

			tfScore := (tf * (bm.k1 + 1)) / (tf + bm.k1*(1-bm.b+bm.b*dl/avgDL))

			docScores[docID] += tfScore * idf
		}
	}

	return bm.rankResults(docScores, queryTokens, limit)
}

func (bm *BM25) avgDocLength() float64 {
	if bm.idx.docCount == 0 {
		return 0
	}
	total := 0
	for _, doc := range bm.idx.documents {
		total += len(doc.Tokens)
	}
	return float64(total) / float64(bm.idx.docCount)
}

func (bm *BM25) rankResults(docScores map[string]float64, queryTokens []string, limit int) []SearchResult {
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
		doc := bm.idx.documents[results[i].docID]
		searchResults = append(searchResults, SearchResult{
			DocID:   doc.ID,
			URL:     doc.URL,
			Title:   doc.Title,
			Score:   results[i].score,
			Snippet: createSnippetBM25(doc.Text, queryTokens),
		})
	}

	return searchResults
}

func createSnippetBM25(text string, queryTokens []string) string {
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
