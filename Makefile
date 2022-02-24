.PHONY: all test install clean fmt vet dependencies gitops gitops-server _docker docker-gitops docker-gitops-server lint ui ui-audit ui-lint ui-test unit-tests  proto proto-deps fakes crd
VERSION=$(shell git describe --always --match "v*")
GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)

BUILD_TIME=$(shell date +'%Y-%m-%d_%T')
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
GIT_COMMIT=$(shell git log -n1 --pretty='%h')
CURRENT_DIR=$(shell pwd)
FORMAT_LIST=$(shell gofmt -l .)
FLUX_VERSION=$(shell $(CURRENT_DIR)/tools/bin/stoml $(CURRENT_DIR)/tools/dependencies.toml flux.version)
LDFLAGS = "-X github.com/weaveworks/weave-gitops/cmd/gitops/version.BuildTime=$(BUILD_TIME) -X github.com/weaveworks/weave-gitops/cmd/gitops/version.Branch=$(BRANCH) -X github.com/weaveworks/weave-gitops/cmd/gitops/version.GitCommit=$(GIT_COMMIT) -X github.com/weaveworks/weave-gitops/pkg/version.FluxVersion=$(FLUX_VERSION) -X github.com/weaveworks/weave-gitops/cmd/gitops/version.Version=$(VERSION)"

KUBEBUILDER_ASSETS ?= "$(CURRENT_DIR)/tools/bin/envtest"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

ifeq ($(BINARY_NAME),)
BINARY_NAME := gitops
endif

##@ Default target
all: gitops ## Install dependencies and build Gitops binary


TEST_TO_RUN=./...
##@ Test
unit-tests: dependencies ## Run unit tests
	which ginkgo || go install github.com/onsi/ginkgo/v2/ginkgo
	# To avoid downloading dependencies every time use `SKIP_FETCH_TOOLS=1 unit-tests`
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS) CGO_ENABLED=0 ginkgo -v -tags unittest $(TEST_TO_RUN)

integration-tests: dependencies
	which ginkgo || go install github.com/onsi/ginkgo/v2/ginkgo
	KUBEBUILDER_ASSETS=$(KUBEBUILDER_ASSETS) CGO_ENABLED=0 ginkgo -v ./test/integration/...

acceptance-tests: local-registry local-docker-image
	which ginkgo || go install github.com/onsi/ginkgo/v2/ginkgo
	IS_TEST_ENV=true IS_LOCAL_REGISTRY=true ginkgo ${ACCEPTANCE_TEST_ARGS} -v ./test/acceptance/test/...

local-kind-cluster-with-registry:
	./tools/kind-with-registry.sh

local-registry:
	./tools/deploy-local-registry.sh

local-docker-image:
	DOCKER_BUILDKIT=1 docker build -t localhost:5000/wego-app:latest . --build-arg FLUX_VERSION=$(FLUX_VERSION)
	docker push localhost:5000/wego-app:latest

test: dependencies
	which ginkgo || go install github.com/onsi/ginkgo/v2/ginkgo
	ginkgo -v ./core/...

fakes: ## Generate testing fakes
	go generate ./...

install: bin ## Install binaries to GOPATH
	cp bin/$(BINARY_NAME) ${GOPATH}/bin/

api-dev: ## Server and watch gitops-server, will reload automatically on change
	reflex -r '.go' -R 'node_modules' -s -- sh -c 'go run -ldflags $(LDFLAGS) cmd/gitops-server/main.go'

cluster-dev: ## Start tilt to do development with wego-app running on the cluster
	tilt up


##@ Build
# In addition to the main file depend on all go files and any other files in
# the cmd directory (e.g. dist, on the assumption that there won't be many)
bin/%: cmd/%/main.go $(shell find . -name "*.go") $(shell find cmd -type f)
ifdef DEBUG
		go build -ldflags $(LDFLAGS) -o $@ $(GO_BUILD_OPTS) $<
else
		go build -ldflags $(LDFLAGS) -gcflags='all=-N -l' -o $@ $(GO_BUILD_OPTS) $<
endif

gitops: bin/gitops ## Build the Gitops CLI, accepts a 'DEBUG' flag
gitops-server: cmd/gitops-server/cmd/dist/index.html bin/gitops-server ## Build the Gitops UI server, accepts a 'DEBUG' flag

DOCKER_REGISTRY?=localhost:5000

_docker:
	DOCKER_BUILDKIT=1 docker build $(DOCKERARGS)\
										-f $(DOCKERFILE) \
										-t $(DOCKER_REGISTRY)/$(subst .dockerfile,,$(DOCKERFILE)):latest \
										.

docker-gitops: DOCKERFILE:=gitops.dockerfile
docker-gitops: _docker ## Build a Docker image of the gitops CLI

docker-gitops-server: DOCKERFILE:=gitops-server.dockerfile
docker-gitops-server: DOCKERARGS:=--build-arg FLUX_VERSION=$(FLUX_VERSION)
docker-gitops-server: _docker ## Build a Docker image of the Gitops UI Server

# Clean up images and binaries
clean: ## Clean up images and binaries
	rm -f bin/*
	rm -rf cmd/gitops-server/cmd/dist
	rm -rf coverage
	rm -rf node_modules
	rm -f .deps
	rm -rf dist
	# There is an important (tracked) file in pkg/flux/bin so don't just nuke the whole folder
	# -x: remove gitignored files too, -d: remove directories too
	git clean -x -d --force pkg/flux/bin/

fmt: ## Run go fmt against code
	go fmt ./...

vet: ## Run go vet against code
	go vet ./...

lint: ## Run linters against code
	golangci-lint run --out-format=github-actions --timeout 600s --skip-files "tilt_modules"

.deps:
	$(CURRENT_DIR)/tools/download-deps.sh $(CURRENT_DIR)/tools/dependencies.toml
	@touch .deps

dependencies: .deps ## Install build dependencies

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"
crd: ## Generate CRDs
	@go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.5.0
	controller-gen $(CRD_OPTIONS) paths="./..." output:crd:artifacts:config=manifests/crds

check-format: ## Check go format
	if [ ! -z "$(FORMAT_LIST)" ] ; then echo invalid format at: ${FORMAT_LIST} && exit 1; fi

proto-deps: ## Update protobuf dependencies
	buf mod update

proto: ## Generate protobuf files
	buf generate
#	This job is complaining about a missing plugin and error-ing out
#	oapi-codegen -config oapi-codegen.config.yaml api/applications/applications.swagger.json

##@ UI

node_modules: ## Install node modules
	npm install-clean
	npx npm-force-resolutions

ui-lint: ## Run linter against the UI
	npm run lint

ui-test: ## Run UI tests
	npm run test

ui-audit: ## Run audit against the UI
	npm audit --production

ui: node_modules cmd/gitops-server/cmd/dist/index.html ## Build the UI

ui-lib: node_modules dist/index.js dist/index.d.ts ## Build UI libraries
# Remove font files from the npm module.
	@find dist -type f -iname \*.otf -delete
	@find dist -type f -iname \*.woff -delete

cmd/gitops-server/cmd/dist/index.html: node_modules $(shell find ui -type f)
# use `mkdir -p` so this works in the ui docker stage
	if [ ! -d "cmd/gitops-server/cmd/dist" ]; then mkdir -p cmd/gitops-server/cmd/dist; fi
	npm run build

# Runs a test to raise errors if the integration between Gitops Core and EE is
# in danger of breaking due to package API changes.
# See the test/library dockerfile and test.sh script for more info.
lib-test: dependencies ## Run the library integration test
	docker build -t gitops-library-test -f test/library/libtest.dockerfile $(CURRENT_DIR)/test/library
	docker run -e GITHUB_TOKEN=$$GITHUB_TOKEN -i --rm \
		-v $(CURRENT_DIR):/go/src/github.com/weaveworks/weave-gitops \
		 gitops-library-test

dist/index.js: ui/index.ts
	npm run build:lib && cp package.json dist

dist/index.d.ts: ui/index.ts
	npm run typedefs

# Test coverage

# JS coverage info
coverage/lcov.info:
	npm run test -- --coverage

# Golang gocov data. Not compatible with coveralls at this point.
unittest.out: dependencies
	go get github.com/ory/go-acc
	go-acc --ignore fakes,acceptance,pkg/api,api,integration -o unittest.out ./... -- -v --timeout=496s -tags test,unittest
	@go mod tidy

integrationtest.out: dependencies
	go get github.com/ory/go-acc
	go-acc --ignore fakes,acceptance,pkg/api,api -o integrationtest.out ./test/integration/... -- -v --timeout=496s -tags test
	@go mod tidy

coverage:
	@mkdir -p coverage

# Convert gocov to lcov for coveralls
coverage/unittest.info: coverage unittest.out
	@go get -u github.com/jandelgado/gcov2lcov
	gcov2lcov -infile=unittest.out -outfile=coverage/unittest.info

coverage/integrationtest.info: coverage integrationtest.out
	gcov2lcov -infile=integrationtest.out -outfile=coverage/integrationtest.info

# Concat the JS and Go coverage files for the coveralls report/
# Note: you need to install `lcov` to run this locally.
# There are no deps listed here to avoid re-running tests. If this fails run the other coverage/ targets first
merged.lcov:
	lcov --add-tracefile coverage/unittest.info --add-tracefile coverage/integrationtest.info -a coverage/lcov.info -o merged.lcov

##@ Utilities

.PHONY: help
# Thanks to https://www.thapaliya.com/en/writings/well-documented-makefiles/
help:  ## Display this help.
ifeq ($(OS),Windows_NT)
				@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n make <target>\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-40s %s\n", $$1, $$2 } /^##@/ { printf "\n%s\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
else
				@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-40s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)
endif
