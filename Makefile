.PHONY: help install-ffmpeg generate-flags build run test clean

# FFmpeg configuration
version ?= n7.0
srcPath = tmp/$(version)/src
patchPath ?=
platform ?=

# FFmpeg configure options
configure = \
	--enable-shared \
	--disable-static \
	--disable-autodetect \
	--disable-programs \
	--disable-doc \
	--disable-postproc \
	--disable-pixelutils \
	--disable-hwaccels \
	--disable-ffprobe \
	--disable-ffplay \
	--enable-openssl \
	--enable-protocol=file,http,hls

# Go build configuration
GO_VERSION := $(shell go version | awk '{print $$3}')
GO_OS := $(shell go env GOOS)
GO_ARCH := $(shell go env GOARCH)

help:
	@echo "Media Proxy - Available targets:"
	@echo "  make install-ffmpeg    - Build and install FFmpeg from source"
	@echo "  make generate-flags    - Generate constant flags from FFmpeg headers"
	@echo "  make build             - Build the media-proxy binary"
	@echo "  make run               - Run the media-proxy server"
	@echo "  make test              - Run tests"
	@echo "  make clean             - Clean build artifacts"
	@echo ""
	@echo "Configuration variables:"
	@echo "  version=<tag>          - FFmpeg version (default: n7.0)"
	@echo "  patchPath=<path>       - Optional patch file to apply"
	@echo "  platform=<name>        - Target platform"

# Install FFmpeg from source
install-ffmpeg:
	@echo "Installing FFmpeg $(version)..."
	rm -rf $(srcPath)
	mkdir -p $(srcPath)
	cd $(srcPath) && git clone https://github.com/FFmpeg/FFmpeg .
	cd $(srcPath) && git checkout $(version)
ifneq "$(patchPath)" ""
	@echo "Applying patch: $(patchPath)"
	cd $(srcPath) && git apply $(patchPath)
endif
	@echo "Configuring FFmpeg..."
	cd $(srcPath) && ./configure --prefix=.. $(configure)
	@echo "Building FFmpeg (using $(shell nproc) cores)..."
	cd $(srcPath) && make -j$(shell nproc)
	@echo "Installing FFmpeg..."
	cd $(srcPath) && make install
	@echo "FFmpeg $(version) installation complete!"
	@echo "Libraries installed to: $(shell pwd)/tmp/$(version)/lib"

# Generate flags from FFmpeg headers
generate-flags:
	@echo "Generating flags from FFmpeg headers..."
	go run internal/cmd/flags/main.go

# Build the media-proxy binary
build: 
	@echo "Building media-proxy ($(GO_OS)/$(GO_ARCH))..."
	@echo "Go version: $(GO_VERSION)"
	CGO_ENABLED=1 go build -ldflags="-s -w" -o bin/media-proxy .
	@echo "Build complete! Binary: bin/media-proxy"

# Run the server
run:
	@echo "Starting media-proxy server..."
	./bin/media-proxy

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -f media-proxy
	@echo "Clean complete!"

# Clean FFmpeg build
clean-ffmpeg:
	@echo "Cleaning FFmpeg build..."
	rm -rf tmp/$(version)
	@echo "FFmpeg clean complete!"

# Full clean (everything)
distclean: clean clean-ffmpeg
	@echo "Full clean complete!"

# Info target to display current configuration
info:
	@echo "=== Build Configuration ==="
	@echo "FFmpeg Version: $(version)"
	@echo "FFmpeg Source Path: $(srcPath)"
	@echo "Patch Path: $(if $(patchPath),$(patchPath),none)"
	@echo ""
	@echo "=== Go Configuration ==="
	@echo "Go Version: $(GO_VERSION)"
	@echo "OS: $(GO_OS)"
	@echo "Architecture: $(GO_ARCH)"
	@echo ""
	@echo "=== FFmpeg Configure Options ==="
	@echo "$(configure)"

# Development setup (install FFmpeg and build)
dev-setup: install-ffmpeg build
	@echo "Development setup complete!"

# Docker build helper
docker-build:
	@echo "Building Docker image..."
	docker build -t media-proxy:latest .
	@echo "Docker build complete!"
