GO ?= go

# the root directory
ROOT_DIR := $(shell dirname $(realpath $(firstword $(MAKEFILE_LIST))))

# bpf source code files
BPF_DIR := $(ROOT_DIR)/bpf

# used for go generate to compile eBPF
BPF_COMPILE := $(ROOT_DIR)/build/clang.sh
BPF_INCLUDE := "-I$(BPF_DIR)/include"

APP_COMMIT ?= $(shell git describe --dirty --long --always)
APP_BUILD_TIME=$(shell date "+%Y%m%d%H%M%S")
APP_VERSION="1.0"

GO_BUILD_STATIC := CGO_ENABLED=1 $(GO) build -tags "netgo osusergo $(GO_TAGS)" -gcflags=all="-N -l" \
	-ldflags "-extldflags -static
GO_BUILD_STATIC_WITH_VERSION := $(GO_BUILD_STATIC) \
	-X main.AppVersion=$(APP_VERSION) \
	-X main.AppGitCommit=$(APP_COMMIT) \
	-X main.AppBuildTime=$(APP_BUILD_TIME)"

# export
export GO_BUILD_STATIC

all: gen-deps gen build tracer

gen-deps:
	# maybe need to install libbpf-devel

gen:
	@BPF_DIR=$(BPF_DIR) \
	BPF_COMPILE=$(BPF_COMPILE) \
	BPF_INCLUDE=$(BPF_INCLUDE) \
	$(GO) generate -x ./...

build:
	$(GO_BUILD_STATIC_WITH_VERSION) -o _output/bin/huatuo-bamai ./cmd/huatuo-bamai

TRACER_DIR := cmd
BIN_DIR := bin

SUBDIRS := $(shell find $(TRACER_DIR) -mindepth 1 -maxdepth 1 -type d -not -path "$(BIN_DIR)" | grep -v 'depend\|huatuo-bamai')
TARGETS := $(patsubst %,$(BIN_DIR)/%,$(notdir $(SUBDIRS)))
COMBINED := $(foreach dir,$(SUBDIRS),$(dir)/$(BIN_DIR)/*.bin)

tracer: $(TARGETS)
$(BIN_DIR)/%: $(TRACER_DIR)/%
	cd $< && make

check: imports fmt golangci-lint

imports:
	@echo "imports"
	@goimports -w -local huatuo-bamai  $(shell find . -type f -name '*.go' -not -path "./vendor/*")

fmt: fmt-rewrite-rules
	@echo "gofumpt"
	gofumpt -l -w $(shell find . -type f -name '*.go' -not -path "./vendor/*")

fmt-rewrite-rules:
	@echo "fmt-rewrite-rules"
	gofmt -w -r 'interface{} -> any' $(shell find . -type f -name '*.go' -not -path "./vendor/*")

golangci-lint:
	@echo "golangci-lint"
	golangci-lint run --build-tags=$(GO_TAGS) -v ./... --timeout=5m --config .golangci.yaml

vendor:
	$(GO) mod tidy
	$(GO) mod verify
	$(GO) mod vendor

clean:
	rm -rf _output $(shell find . -type f -name "*.o") $(COMBINED)

.PHONY: all gen-deps gen build tracer check imports golint fmt golangci-lint vendor clean
