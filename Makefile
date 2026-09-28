.PHONY: build lint test clean

BINARY=better-logging
GOLANGCI_LINT=$(shell go env GOPATH)/bin/golangci-lint

build:
	go build ./...

lint:
	$(GOLANGCI_LINT) run

test:
	go test ./...

clean:
	go clean
	rm -f $(BINARY)