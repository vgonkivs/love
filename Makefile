.PHONY: build clean stream stream-preview view test test-cover test-cover-html help

# Binary name
BINARY := love

# Default values (override with make stream token=xxx)
token ?=
node ?= http://localhost:26658
camera ?= 0
width ?= 1280
height ?= 720
fps ?= 30

# Viewer settings
namespace ?=
start_height ?=
live ?=

# Build the binary
build:
	go build -o $(BINARY) .

# Clean build artifacts
clean:
	rm -f $(BINARY)

# Run streamer without preview
stream: build
	@if [ -z "$(token)" ]; then \
		echo "Error: token is required. Usage: make stream token=<auth_token>"; \
		exit 1; \
	fi
	./$(BINARY) stream \
		-camera $(camera) \
		-width $(width) \
		-height $(height) \
		-fps $(fps) \
		-node $(node) \
		-token $(token) \

# Run streamer with local preview
stream-preview: build
	@if [ -z "$(token)" ]; then \
		echo "Error: token is required. Usage: make stream-preview token=<auth_token>"; \
		exit 1; \
	fi
	./$(BINARY) stream \
		-camera $(camera) \
		-width $(width) \
		-height $(height) \
		-fps $(fps) \
		-node $(node) \
		-token $(token) \

# Run viewer
view: build
	@if [ -z "$(token)" ]; then \
		echo "Error: token is required. Usage: make view token=<auth_token> namespace=<hex> start_height=<height>"; \
		exit 1; \
	fi
	@if [ -z "$(namespace)" ]; then \
		echo "Error: namespace is required. Usage: make view token=<auth_token> namespace=<hex> start_height=<height>"; \
		exit 1; \
	fi
	@if [ -z "$(start_height)" ]; then \
		echo "Error: start_height is required. Usage: make view token=<auth_token> namespace=<hex> start_height=<height>"; \
		exit 1; \
	fi
	./$(BINARY) view \
		-namespace $(namespace) \
		-height $(start_height) \
		-node $(node) \
		-token $(token) \
		$(if $(filter true 1,$(live)),-live,)

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
	@echo "  stream          Run streamer (requires token)"
	@echo "  stream-preview  Run streamer with local preview (requires token)"
	@echo "  view            Run viewer (requires token, namespace, start_height)"
	@echo "  test            Run all tests"
	@echo "  test-cover      Run tests with coverage summary"
	@echo "  test-cover-html Generate HTML coverage report"
	@echo "  help            Show this help"
	@echo ""
	@echo "Variables:"
	@echo "  token           Celestia auth token (required)"
	@echo "  node            Celestia node URL (default: http://localhost:26658)"
	@echo "  camera          Camera device ID (default: 0)"
	@echo "  width           Capture width (default: 1280)"
	@echo "  height          Capture height (default: 720)"
	@echo "  fps             Frames per second (default: 30)"
	@echo ""
	@echo "View variables:"
	@echo "  token           Celestia auth token (required)"
	@echo "  namespace       Stream namespace hex (required)"
	@echo "  start_height    Block height of entrypoint blob (required)"
	@echo "  live            Subscribe to live blobs (set to true for live mode)"
	@echo "  node            Celestia node URL (default: http://localhost:26658)"
	@echo ""
	@echo "Examples:"
	@echo "  make build"
	@echo "  make stream token=eyJhbGc..."
	@echo "  make stream-preview token=eyJhbGc... fps=15 width=640 height=480"
	@echo "  make view token=eyJhbGc... namespace=0a1b2c3d... start_height=1234567"
	@echo "  make view token=eyJhbGc... namespace=0a1b2c3d... start_height=1234567 live=true"
