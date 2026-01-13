.PHONY: build clean stream stream-preview view test test-cover test-cover-html help

# Binary name
BINARY := love

# Default values (override with make stream TOKEN=xxx)
TOKEN ?=
NODE ?= http://localhost:26658
CAMERA ?= 0
WIDTH ?= 1280
HEIGHT ?= 720
FPS ?= 30
QUALITY ?= 85

# Viewer settings
NAMESPACE ?=
START_HEIGHT ?=

# Build the binary
build:
	go build -o $(BINARY) .

# Clean build artifacts
clean:
	rm -f $(BINARY)

# Run streamer without preview
stream: build
	@if [ -z "$(TOKEN)" ]; then \
		echo "Error: TOKEN is required. Usage: make stream TOKEN=<auth_token>"; \
		exit 1; \
	fi
	./$(BINARY) stream \
		-camera $(CAMERA) \
		-width $(WIDTH) \
		-height $(HEIGHT) \
		-fps $(FPS) \
		-quality $(QUALITY) \
		-audio $(AUDIO) \
		-node $(NODE) \
		-token $(TOKEN) \

# Run streamer with local preview
stream-preview: build
	@if [ -z "$(TOKEN)" ]; then \
		echo "Error: TOKEN is required. Usage: make stream-preview TOKEN=<auth_token>"; \
		exit 1; \
	fi
	./$(BINARY) stream \
		-preview \
		-camera $(CAMERA) \
		-width $(WIDTH) \
		-height $(HEIGHT) \
		-fps $(FPS) \
		-quality $(QUALITY) \
		-audio true \
		-node $(NODE) \
		-token $(TOKEN) \

# Run viewer
view: build
	@if [ -z "$(TOKEN)" ]; then \
		echo "Error: TOKEN is required. Usage: make view TOKEN=<auth_token> NAMESPACE=<hex> START_HEIGHT=<height>"; \
		exit 1; \
	fi
	@if [ -z "$(NAMESPACE)" ]; then \
		echo "Error: NAMESPACE is required. Usage: make view TOKEN=<auth_token> NAMESPACE=<hex> START_HEIGHT=<height>"; \
		exit 1; \
	fi
	@if [ -z "$(START_HEIGHT)" ]; then \
		echo "Error: START_HEIGHT is required. Usage: make view TOKEN=<auth_token> NAMESPACE=<hex> START_HEIGHT=<height>"; \
		exit 1; \
	fi
	./$(BINARY) view \
		-namespace $(NAMESPACE) \
		-height $(START_HEIGHT) \
		-node $(NODE) \
		-token $(TOKEN)

# Run tests
test:
	go test ./... -v

# Run tests with coverage
test-cover:
	go test ./... -cover

# Generate HTML coverage report
test-cover-html:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Show help
help:
	@echo "Live Onchain Video Environment"
	@echo ""
	@echo "Targets:"
	@echo "  build           Build the binary"
	@echo "  clean           Remove build artifacts"
	@echo "  stream          Run streamer (requires TOKEN)"
	@echo "  stream-preview  Run streamer with local preview (requires TOKEN)"
	@echo "  view            Run viewer (requires TOKEN, NAMESPACE, START_HEIGHT)"
	@echo "  test            Run all tests"
	@echo "  test-cover      Run tests with coverage summary"
	@echo "  test-cover-html Generate HTML coverage report"
	@echo "  help            Show this help"
	@echo ""
	@echo "Stream variables:"
	@echo "  TOKEN           Celestia auth token (required)"
	@echo "  NODE            Celestia node URL (default: http://localhost:26658)"
	@echo "  CAMERA          Camera device ID (default: 0)"
	@echo "  WIDTH           Capture width (default: 1280)"
	@echo "  HEIGHT          Capture height (default: 720)"
	@echo "  FPS             Frames per second (default: 30)"
	@echo "  QUALITY         JPEG quality 1-100 (default: 85)"
	@echo ""
	@echo "View variables:"
	@echo "  TOKEN           Celestia auth token (required)"
	@echo "  NAMESPACE       Stream namespace hex (required)"
	@echo "  START_HEIGHT    Start block height (required)"
	@echo "  NODE            Celestia node URL (default: http://localhost:26658)"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make stream TOKEN=eyJhbGc..."
	@echo "  make stream-preview TOKEN=eyJhbGc... FPS=15 WIDTH=640 HEIGHT=480"
	@echo "  make view TOKEN=eyJhbGc... NAMESPACE=0a1b2c3d... START_HEIGHT=1234567"
