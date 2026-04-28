# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 BoanLab @ Dankook University

# OutRelay controller + shared library.
# Common targets: build / test / gofmt / golangci-lint / gosec /
# build-image / push-image / clean. See `make help`.

# === Overridable variables ============================================
IMAGE_NAME     ?= outrelay-controller
IMAGE          ?= docker.io/boanlab/$(IMAGE_NAME)
TAG            ?= v0.1.0

GO             ?= go
DOCKER         ?= docker
BIN_DIR        ?= bin

# LDFLAGS strip debug info and stamp the build's version into the
# `main.Version` symbol of each binary. `make TAG=v1.2.3` => version
# string baked at link time; the default is the TAG above.
LDFLAGS        ?= -s -w -X main.Version=$(TAG)
GO_BUILD       ?= CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)'

# Make sure tools we install via `go install` are reachable in the
# same Make invocation: prepend GOBIN (or $GOPATH/bin) to PATH.
GOBIN          ?= $(shell $(GO) env GOBIN)
ifeq ($(GOBIN),)
GOBIN          := $(shell $(GO) env GOPATH)/bin
endif
export PATH    := $(GOBIN):$(PATH)

# Proto sources / generated outputs.
ORP_PROTO_DIR     := api/orp/v1
ORP_PROTO_OUT     := lib/orp/v1
CONTROL_PROTO_DIR := api/control/v1
CONTROL_PROTO_OUT := lib/control/v1

.PHONY: help build test gofmt golangci-lint gosec build-image push-image \
        clean proto dev-pki

.DEFAULT_GOAL := build

help: ## show this help
	@awk 'BEGIN{FS=":.*##"; printf "Targets:\n"} \
	     /^[a-zA-Z][a-zA-Z0-9_-]*:.*##/ { \
	       printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 \
	     }' $(MAKEFILE_LIST)

# === Build ============================================================

build: proto gofmt golangci-lint gosec ## proto + quality gates + compile to ./bin/
	$(GO_BUILD) -o $(BIN_DIR)/outrelay-controller ./cmd/outrelay-controller
	$(GO_BUILD) -o $(BIN_DIR)/outrelay-cli         ./cmd/outrelay-cli
	$(GO_BUILD) -o $(BIN_DIR)/dev-pki              ./tools/dev-pki

# === Quality gates ====================================================

gofmt: ## fail on gofmt drift (use `gofmt -w .` to fix)
	@drift=$$(gofmt -l . 2>&1); \
	if [ -n "$$drift" ]; then \
	  echo "files with gofmt drift:"; echo "$$drift"; \
	  gofmt -d .; \
	  exit 1; \
	fi

golangci-lint: ## golangci-lint run ./... (auto-installs if missing)
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
	  echo "installing golangci-lint into $(GOBIN) ..."; \
	  $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest; \
	fi
	golangci-lint run ./...

gosec: ## gosec security scan (auto-installs if missing)
	@if ! command -v gosec >/dev/null 2>&1; then \
	  echo "installing gosec into $(GOBIN) ..."; \
	  $(GO) install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	gosec -quiet -exclude-generated ./...

test: ## go test -race -count=1 ./...
	$(GO) test -race -count=1 ./...

# === Container image ==================================================

build-image: ## docker build -> $(IMAGE):$(TAG) and $(IMAGE):latest
	$(DOCKER) build -f Dockerfile \
	  -t $(IMAGE):$(TAG) -t $(IMAGE):latest \
	  --build-arg VERSION=$(TAG) .

push-image: build-image ## docker push both $(TAG) and latest
	$(DOCKER) push $(IMAGE):$(TAG)
	$(DOCKER) push $(IMAGE):latest

# === Code generation ==================================================

proto: ## regenerate lib/{orp,control}/v1/*.pb.go (auto-installs protoc-gen-go(-grpc))
	@if ! command -v protoc-gen-go >/dev/null 2>&1; then \
	  echo "installing protoc-gen-go into $(GOBIN) ..."; \
	  $(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@latest; \
	fi
	@if ! command -v protoc-gen-go-grpc >/dev/null 2>&1; then \
	  echo "installing protoc-gen-go-grpc into $(GOBIN) ..."; \
	  $(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest; \
	fi
	@mkdir -p $(ORP_PROTO_OUT) $(CONTROL_PROTO_OUT)
	protoc \
	  --proto_path=$(ORP_PROTO_DIR) \
	  --go_out=$(ORP_PROTO_OUT) --go_opt=paths=source_relative \
	  $(ORP_PROTO_DIR)/*.proto
	protoc \
	  --proto_path=$(CONTROL_PROTO_DIR) \
	  --go_out=$(CONTROL_PROTO_OUT) --go_opt=paths=source_relative \
	  --go-grpc_out=$(CONTROL_PROTO_OUT) --go-grpc_opt=paths=source_relative \
	  $(CONTROL_PROTO_DIR)/*.proto

# === Dev helpers ======================================================

# dev-pki bootstraps a CA + leaf certs for local cluster validation.
# DEV ONLY — leaf TTL is 30 days and agent UUIDs are fixed so manifests
# can pre-bake --uri values.
dev-pki: ## generate ./.dev-pki/{ca,relay-r1,agent-*}.{crt,key} + secrets.yaml
	$(GO) run ./tools/dev-pki -out ./.dev-pki

# === Cleanup ==========================================================

clean: ## rm bin/ dist/ .dev-pki/ + generated *.pb.go
	rm -rf $(BIN_DIR)/ dist/ .dev-pki/
	rm -f $(ORP_PROTO_OUT)/*.pb.go
	rm -f $(CONTROL_PROTO_OUT)/*.pb.go $(CONTROL_PROTO_OUT)/*_grpc.pb.go
