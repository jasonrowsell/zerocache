package cache

import (
	"math/rand"
	"testing"
	"time"
)

func init() {
	// Seed random number generator (used for key generation)
	rand.NewSource(time.Now().UnixNano())
}

// Helper to generate somewhat realistic keys/values
var keyAlphabet = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

func generateKey(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = keyAlphabet[rand.Intn(len(keyAlphabet))]
	}
	return string(b)
}

func generateValue(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 256)
	}
	return b
}

func BenchmarkCacheSet(b *testing.B) {
	c := New() // Default shard count
	keyLen := 16
	valLen := 128
	value := generateValue(valLen)

	// Pre-generate keys outside the timed loop if possible, though N is large
	keys := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		keys[i] = generateKey(keyLen)
	}

	b.ResetTimer() // Start timing now

	// Run the Set operation b.N times
	for i := 0; i < b.N; i++ {
		c.Set(keys[i%len(keys)], value) // Use modulo if keys pre-generated < b.N
	}

	b.StopTimer()
}

func BenchmarkCacheGetHit(b *testing.B) {
	c := New()
	keyLen := 16
	valLen := 128
	key := generateKey(keyLen)
	value := generateValue(valLen)
	c.Set(key, value) // Pre-populate the key

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = c.Get(key) // Repeatedly get the same key
	}
}

func BenchmarkCacheGetMiss(b *testing.B) {
	c := New()
	keyLen := 16
	key := generateKey(keyLen) // A key that doesn't exist

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = c.Get(key)
	}
}

func BenchmarkCacheSetParallel(b *testing.B) {
	c := New()
	keyLen := 16
	valLen := 128
	value := generateValue(valLen)
	numItems := 10000 // Number of unique keys to operate on

	keys := make([]string, numItems)
	for i := range numItems {
		keys[i] = generateKey(keyLen)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine gets its own key index sequence
		keyIndex := rand.Intn(numItems)
		for pb.Next() {
			// Select key pseudo-randomly to hit different shards
			key := keys[keyIndex%numItems]
			c.Set(key, value)
			keyIndex++ // Move to next key for this goroutine
		}
	})
}

func BenchmarkCacheGetParallelHit(b *testing.B) {
	c := New()
	keyLen := 16
	valLen := 128
	numItems := 10000 // Number of unique keys to populate

	keys := make([]string, numItems)
	for i := range numItems {
		key := generateKey(keyLen)
		keys[i] = key
		value := generateValue(valLen)
		c.Set(key, value) // Pre-populate
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		keyIndex := rand.Intn(numItems)
		for pb.Next() {
			key := keys[keyIndex%numItems]
			_, _ = c.Get(key)
			keyIndex++
		}
	})
}

func BenchmarkCacheGetSetParallelMixed(b *testing.B) {
	c := New()
	keyLen := 16
	valLen := 128
	numItems := 10000 // Number of unique keys

	keys := make([]string, numItems)
	for i := range numItems {
		keys[i] = generateKey(keyLen)
	}
	// Pre-populate roughly half
	for i := range numItems / 2 {
		value := generateValue(valLen)
		c.Set(keys[i], value)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		keyIndex := rand.Intn(numItems)
		val := generateValue(valLen) // Generate value once per goroutine
		for pb.Next() {
			key := keys[keyIndex%numItems]
			// Simple 80% Get, 20% Set mix
			if rand.Intn(10) < 8 { // 80% chance GET
				_, _ = c.Get(key)
			} else { // 20% chance SET
				c.Set(key, val)
			}
			keyIndex++
		}
	})
}
