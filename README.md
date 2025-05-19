# ZeroCache

ZeroCache is designed as a fast in-memory cache, similar in spirit to systems like Redis or Memcached but with an initial emphasis on raw read speed and a simpler feature set.

## Getting Started

### Prerequisites

*   Go (version 1.20 or later recommended)
*   `make` (optional, for using the Makefile)

### Building

You can build the server and CLI using the provided Makefile:

```bash
# Build both server and CLI (output to ./bin/)
make build

# Or build individually:
make server
make cli
```

Alternatively, build using standard Go commands:
```bash
# Build the server
go build -o ./bin/zerocached ./cmd/zerocached

# Build the CLI
go build -o ./bin/zerocli ./cmd/zerocli
```
Running the Server (`zerocached`)
```bash
# Start the server with default settings
./bin/zerocached

# Start with custom settings
./bin/zerocached -listen=":7000" -shards=512 -max-items=10000
```
Server Flags:

`-listen`: Address for the cache server to listen on (default: `:6380`).

`-shards`: Number of internal cache shards (must be a power of 2, default: 256).

`-max-items`: Maximum number of items per shard before LRU eviction (0 for unlimited, default: 1024).

Once running, the server will log its startup status.

### Using the CLI (`zerocli`)
Connect to a running zerocached instance:
```bash
# Interactive mode (connects to default 127.0.0.1:6380)
./bin/zerocli

# Connect to a specific server
./bin/zerocli -h 127.0.0.1 -p 7000
```
Inside the CLI:
```bash
127.0.0.1:6380> SET mykey "Hello ZeroCache"
OK
127.0.0.1:6380> GET mykey
"Hello ZeroCache"
127.0.0.1:6380> DEL mykey
OK
127.0.0.1:6380> HELP
ZeroCache CLI Help:
  SET <key> <value>   - Set key to hold the string value.
  GET <key>           - Get the value of key.
  DEL <key>           - Delete a key.
  HELP                - Show this help message.
  QUIT / EXIT         - Disconnect and exit the CLI.
127.0.0.1:6380> QUIT
```

You can also run commands non-interactively:
```bash
./bin/zerocli SET anotherkey somevalue
./bin/zerocli GET anotherkey
```
Running Tests and Benchmarks
Use the Makefile for convenience:
```bash
# Run all unit tests
make test

# Run all benchmarks (this can take a few minutes)
# Includes internal cache benchmarks and end-to-end server benchmarks
make bench
```
Or use go test directly:
```bash
# Test a specific package
go test ./internal/cache

# Benchmark a specific package
go test -bench=. -benchmem ./internal/cache
go test -bench=E2E -benchmem ./cmd/zerocached
```


## Features

*   **In-Memory Storage**: All data is stored in RAM for maximum speed.
*   **Sharded Architecture**:
    *   **Internal Sharding**: The cache data is sharded internally across multiple maps, each protected by its own mutex, to reduce lock contention and improve concurrency on multi-core systems.
    *   **Client-Side Sharding**: A `ShardedClient` is provided to distribute keys across multiple independent ZeroCache server instances, enabling horizontal scaling of throughput and capacity.
*   **Custom Binary Protocol**: A simple, low-overhead binary protocol is used for communication between the client and server to minimize parsing costs.
*   **LRU Eviction**: Implements a Least Recently Used (LRU) eviction policy per shard to manage memory usage when capacity limits are reached.
*   **Low-Latency Focus**: Design choices prioritize reducing latency, including:
    *   Careful memory allocation management (`sync.Pool` for I/O buffers).
    *   `TCP_NODELAY` enabled to reduce network transmission delays.
*   **CLI Tool (`zerocli`)**: An interactive command-line interface similar to `redis-cli` for easy interaction with the cache server.
*   **Go Implementation**: Leverages Go's concurrency primitives (goroutines, channels) and networking libraries.
