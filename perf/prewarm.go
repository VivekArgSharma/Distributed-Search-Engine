package main

import (
	"fmt"
	"net/http"
	"time"
)

func main() {
	client := &http.Client{Timeout: 10 * time.Second}

	queries := []string{"machine", "data", "science", "learning", "algorithm", "computer", "network", "web", "software", "database"}

	fmt.Println("Prewarming cache...")
	for i := 0; i < 10; i++ {
		for _, q := range queries {
			resp, err := client.Get(fmt.Sprintf("http://query-service:8090/search?query=%s&limit=5", q))
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				continue
			}
			resp.Body.Close()
		}
	}
	fmt.Println("Cache prewarmed!")
}
