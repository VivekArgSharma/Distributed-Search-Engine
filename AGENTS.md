# Search Engine - Agent Guidelines

## Project Overview

A Go-based web search engine with crawling, parsing, indexing, and search capabilities using TF-IDF and BM25 ranking algorithms.

## Build Commands

```bash
# Build all packages
go build ./...

# Run all tests
go test ./...

# Run all tests with verbose output
go test ./... -v

# Run specific test
go test ./indexer -v -run TestBM25
go test ./indexer -v -run TestBM25OnSavedIndex

# Run tests in specific package
go test ./indexer -v
go test ./parser -v
go test ./crawler -v

# Build main binary
go build -o search-engine .

# Format code
go fmt ./...

# Vet code
go vet ./...

# Run entire program
go run main.go
```

## Code Structure

```
search-engine/
├── main.go              # Entry point, crawler + search demo
├── indexer/
│   ├── indexer.go       # Core indexing, TF-IDF search, persistence
│   ├── engine.go        # Combines crawler + indexer
│   ├── bm25.go          # BM25 ranking algorithm
│   └── bm25_test.go     # BM25 tests
├── crawler/
│   └── crawler.go       # Concurrent web crawler with workers
├── parser/
│   └── parser.go        # HTML parsing, text/link extraction
└── index_data/          # Serialized index (documents.gob, inverted_index.gob)
```

## Code Style Guidelines

### Imports
- Group imports: stdlib first, then third-party, then internal
- Use blank line between groups
- Example:
```go
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
```

### Naming Conventions
- **Types**: PascalCase (e.g., `Indexer`, `Document`, `SearchResult`)
- **Functions/Methods**: PascalCase (e.g., `NewIndexer`, `Search`)
- **Variables/Fields**: camelCase (e.g., `docCount`, `invertedIndex`)
- **Constants**: PascalCase or camelCase based on usage (e.g., `maxWorkers`, `stopWords`)
- **Packages**: lowercase, short (e.g., `indexer`, `crawler`)
- **Acronyms**: follow Go conventions (e.g., `URL`, `ID` - all caps or lower depending on position)

### Types and Structs
- Embed mutexes in structs that need thread safety
- Use pointer receivers for methods on structs with mutex fields
- Document exported types with comments:
```go
// Document represents an indexed webpage
type Document struct {
    ID     string
    URL    string
    Title  string
    Text   string
    Tokens []string
}
```

### Error Handling
- Return errors from functions, don't log and continue (unless in main/demo)
- Check errors immediately after operations
- Use early returns to reduce nesting
```go
func (idx *Indexer) Load(dir string) error {
    docsFile, err := os.Open(filepath.Join(dir, "documents.gob"))
    if err != nil {
        return err
    }
    defer docsFile.Close()
    // ...
}
```

### Concurrency
- Use `sync.RWMutex` for read-heavy workloads (indexer)
- Use `defer mu.Unlock()` to ensure mutex release
- Acquire read lock (`RLock`) for reads, write lock (`Lock`) for writes
```go
func (idx *Indexer) Search(query string, limit int) []SearchResult {
    idx.mu.RLock()
    defer idx.mu.RUnlock()
    // ...
}
```

### Testing
- Test files named `<package>_test.go`
- Test functions prefixed with `Test`
- Use table-driven tests when appropriate
- Use `t.Skipf` for tests requiring external resources
- Print results in tests for debugging:
```go
fmt.Println("\n=== BM25 Search: 'concurrency' ===")
for _, r := range results {
    fmt.Printf("[%.2f] %s\n", r.Score, r.Title)
}
```

### Comments
- Comment before the declaration, not after
- Use complete sentences with proper capitalization
- Don't comment obvious code
```go
// tokenize splits text into lowercase tokens, filtering stopwords and short words
func (idx *Indexer) tokenize(text string) []string {
```

## Key Design Patterns

### Indexer Architecture
- `documents`: map[string]*Document - docID to document
- `invertedIndex`: map[string][]Posting - term to posting lists
- `docCount`: total number of indexed documents
- Uses gob serialization for persistence

### Search Result Flow
1. Tokenize query
2. Look up postings for each token
3. Calculate scores (TF-IDF or BM25)
4. Sort by score descending
5. Return top N results with snippets

### Crawler Architecture
- Worker pool pattern with configurable workers
- Channel-based job queue (`jobs`, `pages`)
- Rate limiting per worker
- Depth-limited crawling
- Wikipedia-specific link filtering

## Dependencies

- `golang.org/x/net/html` - HTML parsing
- Standard library: `encoding/gob`, `sync`, `regexp`, `strings`, `math`, `os`, `path/filepath`, `io`, `net/http`, `net/url`, `time`

## Configuration

- `regexp.MustCompile(\`[a-zA-Z0-9]+\`)` - Tokenizer pattern (defined in `NewIndexer`)
- `stopWords` map - Filtered during tokenization
- BM25 parameters: `k1=1.5`, `b=0.75`