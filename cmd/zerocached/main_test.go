// cmd/zerocached/main_test.go
package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	zcCache "github.com/jasonrowsell/zerocache/internal/cache"
	zcServer "github.com/jasonrowsell/zerocache/internal/server"
	zcClient "github.com/jasonrowsell/zerocache/pkg/client"
)

const benchmarkServerAddr = "127.0.0.1:6381"

var (
	benchServerOnce sync.Once
	benchClient     *zcClient.Client
	serverErrChan   chan error
)

// startBenchmarkServer starts a test server instance.
func startBenchmarkServer() {
	// Disable logging unless debugging tests
	log.SetOutput(io.Discard)

	serverErrChan = make(chan error, 1)
	c := zcCache.New() // Use default settings
	srv := zcServer.New(c)

	go func() {
		err := srv.ListenAndServe(benchmarkServerAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Benchmark server ListenAndServe error: %v\n", err)
			serverErrChan <- err // Send error back
		}
		close(serverErrChan) // Close channel when server stops
	}()

	// Wait for server readiness
	maxWait := 5 * time.Second
	pollInterval := 100 * time.Millisecond
	startTime := time.Now()
	var readyConn net.Conn
	var lastDialErr error
	for time.Since(startTime) < maxWait {
		select {
		case err, ok := <-serverErrChan:
			if ok && err != nil {
				panic(fmt.Sprintf("Benchmark server failed to start: %v", err))
			}
			if !ok {
				panic("Benchmark server exited prematurely without successful connection")
			}
		default: // Non-blocking check
		}
		conn, dialErr := net.DialTimeout("tcp", benchmarkServerAddr, pollInterval/2)
		if dialErr == nil {
			fmt.Println("Polling: Connection successful.")
			readyConn = conn
			goto ServerReady
		}
		lastDialErr = dialErr
		time.Sleep(pollInterval)
	}
ServerReady:
	if readyConn == nil {
		panic(fmt.Sprintf("Benchmark server (%s) did not become ready within %v. Last dial error: %v", benchmarkServerAddr, maxWait, lastDialErr))
	}

	fmt.Println("Benchmark server is ready.")

	// Create setup client using the established connection
	var err error
	benchClient, err = zcClient.NewWithConn(readyConn)
	if err != nil {
		readyConn.Close()
		panic(fmt.Sprintf("Failed to create benchmark setup client with existing connection: %v", err))
	}
	fmt.Println("Benchmark setup client created successfully.")
}

// TestMain ensures the server is started before running benchmarks.
func TestMain(m *testing.M) {
	fmt.Println("Setting up benchmark server...")
	benchServerOnce.Do(startBenchmarkServer)

	// Run the benchmarks and tests
	exitCode := m.Run()

	// Cleanup after tests
	fmt.Println("Closing benchmark setup client...")
	if benchClient != nil {
		benchClient.Close()
	}

	fmt.Println("Benchmark run finished.")
	os.Exit(exitCode)
}

var keyAlphabetBench = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

// Creates a new random source for each call to avoid global lock contention if called concurrently.
func newRandSource() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

func generateKeyBench(r *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = keyAlphabetBench[r.Intn(len(keyAlphabetBench))]
	}
	return string(b)
}

func generateValueBench(r *rand.Rand, n int) []byte {
	b := make([]byte, n)
	_, _ = r.Read(b) // Fill with pseudo-random bytes
	return b
}

func BenchmarkE2ESet(b *testing.B) {
	keyLen := 16
	valLen := 128

	// Pre-generate keys and a single value to reduce overhead inside the loop
	value := generateValueBench(newRandSource(), valLen)
	keys := make([]string, b.N)
	localRand := newRandSource()
	for i := 0; i < b.N; i++ {
		keys[i] = generateKeyBench(localRand, keyLen)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		cli, err := zcClient.New(benchmarkServerAddr)
		if err != nil {
			b.Fatalf("Failed to create client in parallel benchmark: %v", err)
		}
		defer cli.Close()

		// Each goroutine gets its own random source for index selection
		r := newRandSource()
		keyIndex := r.Intn(b.N) // Initial random start index

		for pb.Next() {
			// Use modulo on b.N which matches keys length
			key := keys[keyIndex%b.N]
			err := cli.Set(key, value) // Use pre-generated value
			if err != nil {
				b.Errorf("Set failed for key %s: %v", key, err)
			}
			keyIndex++ // Simple linear scan after random start
		}
	})
}

func BenchmarkE2EGetHit(b *testing.B) {
	keyLen := 16
	valLen := 128
	numItems := 10000 // Fixed set of keys

	// Pre-populate using the setup client
	keys := make([]string, numItems)
	localRand := newRandSource()
	value := generateValueBench(localRand, valLen)

	fmt.Printf("Pre-populating %d keys for GetHit benchmark...\n", numItems)
	startPopulate := time.Now()
	populateRand := newRandSource() // Use dedicated rand for key generation during population
	for i := 0; i < numItems; i++ {
		key := generateKeyBench(populateRand, keyLen)
		keys[i] = key
		err := benchClient.Set(key, value) // Use setup client
		if err != nil {
			b.Fatalf("Failed to pre-populate key %s [%d/%d]: %v", key, i+1, numItems, err)
		}
	}
	fmt.Printf("Finished pre-populating for GetHit in %v\n", time.Since(startPopulate))

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		cli, err := zcClient.New(benchmarkServerAddr)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}
		defer cli.Close()

		r := newRandSource()
		keyIndex := r.Intn(numItems) // Index into the populated keys slice

		for pb.Next() {
			key := keys[keyIndex%numItems] // Select from populated keys
			_, err := cli.Get(key)
			if err != nil {
				if err == zcClient.ErrNotFound {
					b.Errorf("GetHit expected hit but got ErrNotFound for key %s", key)
				} else {
					b.Errorf("GetHit failed for key %s: %v", key, err)
				}
			}
			keyIndex++
		}
	})
}

func BenchmarkE2EGetMiss(b *testing.B) {
	keyLen := 16

	// Generate keys known not to exist
	keys := make([]string, b.N)
	localRand := newRandSource()
	for i := 0; i < b.N; i++ {
		keys[i] = "miss_" + generateKeyBench(localRand, keyLen)
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		cli, err := zcClient.New(benchmarkServerAddr)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}
		defer cli.Close()

		r := newRandSource()
		keyIndex := r.Intn(b.N)

		for pb.Next() {
			key := keys[keyIndex%b.N]
			_, err := cli.Get(key)
			if err != zcClient.ErrNotFound {
				b.Errorf("GetMiss expected ErrNotFound but got: %v (key: %s)", err, key)
			}
			keyIndex++
		}
	})
}

func BenchmarkE2EDelete(b *testing.B) {
	keyLen := 16
	valLen := 128     // Value length doesn't matter for delete, but used for population
	numItems := 10000 // Number of items to pre-populate for deletion

	// Pre-populate using the setup client - Need unique keys per run
	// Otherwise, later benchmark runs might try deleting already deleted keys.
	// Generate a unique prefix for this benchmark run's keys.
	runPrefix := fmt.Sprintf("delete_run_%d_", time.Now().UnixNano())
	keys := make([]string, numItems)
	localRand := newRandSource()
	value := generateValueBench(localRand, valLen) // Value needed for Set

	fmt.Printf("Pre-populating %d keys for Delete benchmark (prefix: %s)...\n", numItems, runPrefix)
	startPopulate := time.Now()
	populateRand := newRandSource()
	for i := 0; i < numItems; i++ {
		key := runPrefix + generateKeyBench(populateRand, keyLen)
		keys[i] = key
		err := benchClient.Set(key, value) // Use setup client
		if err != nil {
			b.Fatalf("Failed to pre-populate key %s for Delete benchmark: %v", key, err)
		}
	}
	fmt.Printf("Finished pre-populating for Delete in %v\n", time.Since(startPopulate))

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		cli, err := zcClient.New(benchmarkServerAddr)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}
		defer cli.Close()

		r := newRandSource()
		// Index into the keys slice that we just populated
		keyIndex := r.Intn(numItems)

		for pb.Next() {
			// Select a key from the pre-populated set for this run
			key := keys[keyIndex%numItems]
			err := cli.Delete(key)
			if err != nil {
				// Deleting an existing key shouldn't normally error.
				// It's possible another parallel goroutine deleted it first in theory,
				// but unlikely to be the dominant case. Treat errors as failures.
				b.Errorf("Delete failed for key %s: %v", key, err)
			}
			// Advance index. If keys run out, we might try deleting already deleted keys,
			// which is okay for Delete benchmark (should be fast no-op on server).
			keyIndex++
		}
	})
}

// BenchmarkE2EMixed simulates a mixed GET/SET workload (e.g., 80% GET, 20% SET).
func BenchmarkE2EMixed(b *testing.B) {
	keyLen := 16
	valLen := 128
	numItems := 10000 // Total keyspace size
	readRatio := 0.80 // 80% GET operations

	// Pre-populate roughly half the keyspace initially
	keys := make([]string, numItems)
	localRand := newRandSource()
	value := generateValueBench(localRand, valLen) // Value for setting

	fmt.Printf("Pre-populating ~%d keys for Mixed benchmark...\n", int(float64(numItems)*(1.0-readRatio))) // Populating roughly the write ratio
	startPopulate := time.Now()
	populateRand := newRandSource()
	for i := 0; i < numItems; i++ {
		key := generateKeyBench(populateRand, keyLen)
		keys[i] = key
		// Populate a subset, e.g., keys with even index, or randomly ~20%
		if populateRand.Float64() > readRatio { // Populate roughly (1 - readRatio) %
			err := benchClient.Set(key, value)
			if err != nil {
				b.Fatalf("Failed to pre-populate key %s for Mixed benchmark: %v", key, err)
			}
		}
	}
	fmt.Printf("Finished pre-populating for Mixed in %v\n", time.Since(startPopulate))

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		cli, err := zcClient.New(benchmarkServerAddr)
		if err != nil {
			b.Fatalf("Failed to create client: %v", err)
		}
		defer cli.Close()

		// Each goroutine needs its own random source and value buffer
		r := newRandSource()
		localValue := generateValueBench(r, valLen) // Value for SETs in this goroutine
		keyIndex := r.Intn(numItems)

		for pb.Next() {
			key := keys[keyIndex%numItems] // Select a key from the overall keyspace

			// Perform GET or SET based on ratio
			if r.Float64() < readRatio { // Read path (GET)
				_, err := cli.Get(key)
				// Ignore ErrNotFound in mixed workload, as keys might not exist yet or were deleted implicitly
				if err != nil && err != zcClient.ErrNotFound {
					b.Errorf("Mixed workload GET failed for key %s: %v", key, err)
				}
			} else { // Write path (SET)
				err := cli.Set(key, localValue)
				if err != nil {
					b.Errorf("Mixed workload SET failed for key %s: %v", key, err)
				}
			}
			keyIndex++
		}
	})
}
