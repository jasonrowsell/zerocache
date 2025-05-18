package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jasonrowsell/zerocache/internal/cache"
	"github.com/jasonrowsell/zerocache/internal/server"
)

var (
	listenAddr       = flag.String("listen", ":6380", "Address to listen on (e.g., :6380 or 127.0.0.1:6380)")
	shardCount       = flag.Int("shards", 256, "Number of cache shards (must be power of 2)")
	maxItemsPerShard = flag.Int("max-items", 1024, "Max items per shard (0 for unlimited)")
)

func main() {
	flag.Parse()

	// Validate shardCount is power of 2
	if *shardCount <= 0 || (*shardCount&(*shardCount-1)) != 0 {
		log.Fatalf("Error: shard count (-shards=%d) must be a positive power of 2.", *shardCount)
	}
	if *maxItemsPerShard < 0 {
		log.Fatalf("Error: max items per shard (-max-items=%d) cannot be negative.", *maxItemsPerShard)
	}

	log.Println("Starting ZeroCache server...")
	log.Printf("Configuration: Listen Addr=%s, Shards=%d, MaxItems/Shard=%d", *listenAddr, *shardCount, *maxItemsPerShard)

	cacheConfig := cache.Config{
		ShardCount:       *shardCount,
		MaxItemsPerShard: *maxItemsPerShard,
	}
	c := cache.NewWithConfig(cacheConfig)

	svr := server.New(c)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := svr.ListenAndServe(*listenAddr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	log.Println("Server started successfully.")
	<-sigChan
	log.Println("Shutdown signal received, shutting down...")

	log.Println("ZeroCache server stopped.")
}
