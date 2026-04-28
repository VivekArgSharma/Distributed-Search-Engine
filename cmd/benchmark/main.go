package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	host := os.Getenv("BENCHMARK_HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	hotQueries := []string{"machine", "data", "science", "learning"}
	coldQueries := []string{"algorithm", "computer", "network", "web", "software", "database", "python", "java"}

	client := &http.Client{Timeout: 10 * time.Second}

	duration := 10 * time.Second
	finish := time.Now().Add(duration)

	// Test with 70% hot queries
	hotRatio := 0.7
	workers := 75

	var total, hotCount, coldCount int64
	var latencies []float64
	var mu sync.Mutex

	fmt.Printf("Benchmarking against %s:8090\n", host)

	for i := 0; i < workers; i++ {
		go func() {
			for time.Now().Before(finish) {
				var q string
				if rand.Float64() < hotRatio {
					q = hotQueries[rand.Intn(len(hotQueries))]
					atomic.AddInt64(&hotCount, 1)
				} else {
					q = coldQueries[rand.Intn(len(coldQueries))]
					atomic.AddInt64(&coldCount, 1)
				}

				start := time.Now()
				resp, err := client.Get(fmt.Sprintf("http://%s:8090/search?query=%s&limit=5", host, q))
				elapsed := time.Since(start).Milliseconds()

				if err == nil {
					resp.Body.Close()
					atomic.AddInt64(&total, 1)
					mu.Lock()
					latencies = append(latencies, float64(elapsed))
					mu.Unlock()
				}
			}
		}()
	}

	time.Sleep(duration)

	qps := float64(total) / duration.Seconds()
	var p50, p95, p99 float64
	if len(latencies) > 0 {
		sort.Float64s(latencies)
		p50 = latencies[len(latencies)*50/100]
		p95 = latencies[len(latencies)*95/100]
		p99 = latencies[len(latencies)*99/100]
	}

	fmt.Println("=== Mixed Query Benchmark (70% hot / 30% cold) ===")
	fmt.Printf("QPS: %.0f\n", qps)
	fmt.Printf("p50: %.0fms, p95: %.0fms, p99: %.0fms\n", p50, p95, p99)
	fmt.Printf("Hot: %d, Cold: %d\n", hotCount, coldCount)
}
