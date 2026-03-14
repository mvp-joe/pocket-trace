.PHONY: build clean dev

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	cd ui && npm run build
	rm -rf cmd/pocket-trace/ui/dist
	mkdir -p cmd/pocket-trace/ui
	cp -r ui/dist cmd/pocket-trace/ui/dist
	go build -ldflags "-X main.version=$(VERSION)" -o pocket-trace ./cmd/pocket-trace

clean:
	rm -f pocket-trace
	rm -rf ui/dist
	rm -rf cmd/pocket-trace/ui

dev:
	go build -tags dev -o pocket-trace ./cmd/pocket-trace
