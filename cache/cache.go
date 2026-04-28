package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"search-engine/indexer"
)

type Cache struct {
	client *redis.Client
	ttl    time.Duration
}

type CacheResult struct {
	Results []indexer.SearchResult `json:"results"`
	Took    string                 `json:"took"`
}

func NewCache(addr string, ttl time.Duration) *Cache {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		PoolSize:     50,
		MinIdleConns: 10,
	})
	return &Cache{
		client: client,
		ttl:    ttl,
	}
}

func (c *Cache) generateKey(query string, mode indexer.SearchMode, limit int) string {
	raw := fmt.Sprintf("%s:%s:%d", query, mode, limit)
	hash := sha256.Sum256([]byte(raw))
	return "search:" + hex.EncodeToString(hash[:])
}

func (c *Cache) Get(ctx context.Context, query string, mode indexer.SearchMode, limit int) (*CacheResult, error) {
	key := c.generateKey(query, mode, limit)

	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	var result CacheResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func (c *Cache) Set(ctx context.Context, query string, mode indexer.SearchMode, limit int, result *CacheResult) error {
	key := c.generateKey(query, mode, limit)

	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, key, data, c.ttl).Err()
}

func (c *Cache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

func (c *Cache) Close() error {
	return c.client.Close()
}
