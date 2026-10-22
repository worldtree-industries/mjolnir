.PHONY: build lint lint-workflows install-lint install-golangci-lint install-actionlint setup-hooks test test-race vet verify clean

BINARY=mjolnir
GOLANGCI_LINT=$(shell go env GOPATH)/bin/golangci-lint
GOLANGCI_VERSION=v2.14 # the only pins; CI installs the linters from here
ACTIONLINT=$(shell go env GOPATH)/bin/actionlint
ACTIONLINT_VERSION=v1.7.12

build:
	go build ./...

install-lint: install-golangci-lint install-actionlint

install-golangci-lint:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

install-actionlint:
	go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)

setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit

lint:
	@[ -x "$(GOLANGCI_LINT)" ] || { echo "golangci-lint $(GOLANGCI_VERSION) is not installed: run 'make install-lint'"; exit 1; }
	$(GOLANGCI_LINT) run

lint-workflows:
	@[ -x "$(ACTIONLINT)" ] || { echo "actionlint $(ACTIONLINT_VERSION) is not installed: run 'make install-lint'"; exit 1; }
	$(ACTIONLINT) -color .github/workflows/*.yml

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

# The gate: everything a change must pass before it counts as green. Every
# caller (pre-commit, the PR workflow, the release workflow) runs this target.
verify: build vet lint lint-workflows test-race

clean:
	go clean
	rm -f $(BINARY)
