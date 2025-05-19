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
./bin/zerocached -listen=":7000" -metrics=":9200" -shards=512 -max-items=10000
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