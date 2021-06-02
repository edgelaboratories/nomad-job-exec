GO_BUILD_OPTS =

all: fmt build test

.PHONY: fmt
fmt:
	go mod tidy
	gofumpt -s -w .
	gofumports -w .

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: build
build:
	CGO_ENABLED=0 go build $(GO_BUILD_OPTS) -o bin/nomad-alloc-exec .

.PHONY: test
test:
	go test --race -coverprofile=bin/cover.out ./...
	go tool cover -html=bin/cover.out -o bin/cover.html
