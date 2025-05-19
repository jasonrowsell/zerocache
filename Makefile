.PHONY: all build server cli test bench clean help

APP_NAME_SERVER := zerocached
APP_NAME_CLI := zerocli
CMD_SERVER_PATH := ./cmd/$(APP_NAME_SERVER)
CMD_CLI_PATH := ./cmd/$(APP_NAME_CLI)
OUTPUT_DIR := ./bin

GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOCLEAN := $(GOCMD) clean
GOMODTIDY := $(GOCMD) mod tidy

all: build

build: server cli

server:
	@echo "Building $(APP_NAME_SERVER)..."
	@mkdir -p $(OUTPUT_DIR)
	$(GOBUILD) -o $(OUTPUT_DIR)/$(APP_NAME_SERVER) $(CMD_SERVER_PATH)
	@echo "$(APP_NAME_SERVER) built in $(OUTPUT_DIR)/"

cli:
	@echo "Building $(APP_NAME_CLI)..."
	@mkdir -p $(OUTPUT_DIR)
	$(GOBUILD) -o $(OUTPUT_DIR)/$(APP_NAME_CLI) $(CMD_CLI_PATH)
	@echo "$(APP_NAME_CLI) built in $(OUTPUT_DIR)/"

test:
	@echo "Running unit tests..."
	$(GOTEST) -v ./...

# Run benchmarks for all packages (can be slow, adjust as needed)
# Add -count=N for multiple runs
bench:
	@echo "Running benchmarks..."
	@echo "Running cache benchmarks..."
	$(GOTEST) -bench=. -benchmem ./internal/cache
	@echo "\nRunning E2E benchmarks (this will start a test server)..."
	$(GOTEST) -bench=E2E -benchmem ./cmd/zerocached -count=1

clean:
	@echo "Cleaning up..."
	$(GOCLEAN) -testcache
	@rm -rf $(OUTPUT_DIR)
	@echo "Cleaned."

tidy:
	@echo "Tidying go modules..."
	$(GOMODTIDY)

help:
	@echo "Available targets:"
	@echo "  all         - Build all applications (default)"
	@echo "  build       - Build all applications"
	@echo "  server      - Build the ZeroCache server ($(APP_NAME_SERVER))"
	@echo "  cli         - Build the ZeroCache CLI ($(APP_NAME_CLI))"
	@echo "  test        - Run unit tests for all packages"
	@echo "  bench       - Run benchmark tests"
	@echo "  clean       - Remove build artifacts and test cache"
	@echo "  tidy        - Run 'go mod tidy'"
	@echo "  help        - Show this help message"