.PHONY: build lint install-lint setup-hooks test clean

BINARY=mjolnir
GOLANGCI_LINT=$(shell go env GOPATH)/bin/golangci-lint
GOLANGCI_VERSION=v2.14 # keep in sync with .github/workflows/ci.yml

build:
	go build ./...

install-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit

lint:
	$(GOLANGCI_LINT) run

test:
	go test ./...

clean:
	go clean
	rm -f $(BINARY)
