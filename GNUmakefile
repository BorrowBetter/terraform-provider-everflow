# GNUmakefile — developer ergonomics for terraform-provider-everflow
#
# Common targets:
#   make build      — compile the provider binary
#   make install    — build and drop the binary where Terraform's
#                     dev_overrides can pick it up
#   make test       — unit tests with race detector
#   make testacc    — acceptance tests (requires TF_ACC=1 env and real API)
#   make docs       — regenerate docs/ via tfplugindocs
#   make lint       — go vet + gofmt check

HOSTNAME    = registry.terraform.io
NAMESPACE   = BorrowBetter
NAME        = everflow
BINARY      = terraform-provider-$(NAME)
VERSION     = 0.0.1-dev
OS_ARCH    := $(shell go env GOOS)_$(shell go env GOARCH)
INSTALL_DIR = $(HOME)/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)

.PHONY: default build install test testacc docs lint fmt tidy clean

default: build

build:
	go build -o $(BINARY)

install: build
	mkdir -p $(INSTALL_DIR)
	mv $(BINARY) $(INSTALL_DIR)/$(BINARY)

test:
	go test ./... -race -timeout 300s

testacc:
	TF_ACC=1 go test ./internal/provider -v -race -timeout 120m

TFPLUGINDOCS_VERSION = v0.22.0
docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) generate --provider-name everflow

lint:
	go vet ./...
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "gofmt found unformatted files:"; \
		gofmt -l .; \
		exit 1; \
	fi

fmt:
	gofmt -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
	rm -rf dist/
