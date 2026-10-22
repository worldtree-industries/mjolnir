.PHONY: build lint install-lint setup-hooks test test-race vet verify clean

BINARY=mjolnir
GOLANGCI_LINT=$(shell go env GOPATH)/bin/golangci-lint
GOLANGCI_VERSION=v2.14 # the only pin; CI installs the linter from here

build:
	go build ./...

install-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit

lint:
	@[ -x "$(GOLANGCI_LINT)" ] || { echo "golangci-lint $(GOLANGCI_VERSION) is not installed: run 'make install-lint'"; exit 1; }
	$(GOLANGCI_LINT) run

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

# The gate: everything a change must pass before it counts as green. Every
# caller (pre-commit, the PR workflow, the release workflow) runs this target.
verify: build vet lint test-race

clean:
	go clean
	rm -f $(BINARY)
