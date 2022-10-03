#
# Copyright The CloudNativePG Contributors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

# Image URL to use all building/pushing image targets

# Prevent e2e tests to proceed with empty tag which
# will be considered as "latest".
ifeq (,$(CONTROLLER_IMG))
IMAGE_TAG = $(shell (git symbolic-ref -q --short HEAD || git describe --tags --exact-match) | tr / -)
ifneq (,${IMAGE_TAG})
CONTROLLER_IMG = ghcr.io/cloudnative-pg/cloudnative-pg-testing:${IMAGE_TAG}
endif
endif

COMMIT := $(shell git rev-parse --short HEAD || echo unknown)
DATE := $(shell git log -1 --pretty=format:'%ad' --date short)
VERSION := $(shell git describe --tags --match 'v*' | sed -e 's/^v//; s/-g[0-9a-f]\+$$//; s/-\([0-9]\+\)$$/+dev\1/')
LDFLAGS= "-X github.com/cloudnative-pg/cloudnative-pg/pkg/versions.buildVersion=${VERSION} $\
-X github.com/cloudnative-pg/cloudnative-pg/pkg/versions.buildCommit=${COMMIT} $\
-X github.com/cloudnative-pg/cloudnative-pg/pkg/versions.buildDate=${DATE}"
OPERATOR_MANIFEST_PATH := $(shell pwd)/dist/operator-manifest.yaml

BUILD_IMAGE ?= true
POSTGRES_IMAGE_NAME ?= ghcr.io/cloudnative-pg/postgresql:14
KUSTOMIZE_VERSION ?= v4.5.2
KIND_CLUSTER_NAME ?= pg
KIND_CLUSTER_VERSION ?= v1.25.0
CONTROLLER_TOOLS_VERSION ?= v0.9.2
GORELEASER_VERSION ?= v1.10.3

export CONTROLLER_IMG
export BUILD_IMAGE
export POSTGRES_IMAGE_NAME
export OPERATOR_MANIFEST_PATH
# We don't need `trivialVersions=true` anymore, with `crd` it's ok for multi versions
CRD_OPTIONS ?= "crd"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

ENVTEST_ASSETS_DIR=$$(pwd)/testbin
test: generate fmt vet manifests ## Run tests.
	mkdir -p ${ENVTEST_ASSETS_DIR} ;\
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest ;\
	source <(setup-envtest use -p env --bin-dir ${ENVTEST_ASSETS_DIR} ${ENVTEST_K8S_VERSION}) ;\
	go test -coverpkg=./... --count=1 -coverprofile=cover.out ./api/... ./cmd/... ./controllers/... ./internal/... ./pkg/... ./tests/utils ;

e2e-test-kind: ## Run e2e tests locally using kind.
	hack/e2e/run-e2e-kind.sh

e2e-test-k3d: ## Run e2e tests locally using k3d.
	hack/e2e/run-e2e-k3d.sh

##@ Build
build: generate fmt vet ## Build binaries.
	go build -o bin/manager -ldflags ${LDFLAGS} ./cmd/manager
	go build -o bin/kubectl-cnpg -ldflags ${LDFLAGS} ./cmd/kubectl-cnpg

run: generate fmt vet manifests ## Run against the configured Kubernetes cluster in ~/.kube/config.
	go run ./cmd/manager

docker-build: go-releaser ## Build the docker image.
	GOOS=linux GOARCH=amd64 DATE=${DATE} COMMIT=${COMMIT} VERSION=${VERSION} \
	  $(GO_RELEASER) build --skip-validate --rm-dist --single-target
	DOCKER_BUILDKIT=1 docker build . -t ${CONTROLLER_IMG} --build-arg VERSION=${VERSION}

docker-push: ## Push the docker image.
	docker push ${CONTROLLER_IMG}

##@ Deployment
install: manifests kustomize ## Install CRDs into a cluster.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from a cluster.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: ## Deploy controller in the configured Kubernetes cluster in ~/.kube/config.
## Generate the operator-manifest.yaml if it's not already there
ifeq (,$(wildcard $(OPERATOR_MANIFEST_PATH)))
	@make generate-manifest
endif
	kubectl apply -f ${OPERATOR_MANIFEST_PATH}

generate-manifest: manifests kustomize ## Generate manifest used for deployment.
	set -e ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	cp -r config/* $$CONFIG_TMP_DIR ;\
	{ \
		cd $$CONFIG_TMP_DIR/default ;\
		$(KUSTOMIZE) edit add patch --path manager_image_pull_secret.yaml ;\
		cd $$CONFIG_TMP_DIR/manager ;\
		$(KUSTOMIZE) edit set image controller="${CONTROLLER_IMG}" ;\
		$(KUSTOMIZE) edit add patch --path env_override.yaml ;\
		$(KUSTOMIZE) edit add configmap controller-manager-env \
			--from-literal="POSTGRES_IMAGE_NAME=${POSTGRES_IMAGE_NAME}" ;\
	} ;\
	$(KUSTOMIZE) build $$CONFIG_TMP_DIR/default > ${OPERATOR_MANIFEST_PATH} ;\
	rm -fr $$CONFIG_TMP_DIR

manifests: controller-gen ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

deploy-locally: kind-cluster ## Build and deploy operator in local cluster
	set -e ;\
	hack/setup-cluster.sh -n1 -r load deploy

##@ Formatters and Linters

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

lint: ## Run the linter.
	golangci-lint run

shellcheck: ## Shellcheck for the hack directory.
	@{ \
	set -e ;\
	find -name '*.sh' -exec shellcheck -a -S style {} + ;\
	}

spellcheck: ## Runs the spellcheck on the project.
	docker run --rm -v $(PWD):/tmp jonasbn/github-action-spellcheck:0.25.0

woke: ## Runs the woke checks on project.
	docker run --rm -v $(PWD):/src -w /src getwoke/woke:0.18.1 woke -c .woke.yaml

wordlist-ordered: ## Order the wordlist using sort
	LANG=C LC_ALL=C sort .wordlist-en-custom.txt > .wordlist-en-custom.txt.new && \
	mv -f .wordlist-en-custom.txt.new .wordlist-en-custom.txt

checks: generate manifests apidoc fmt spellcheck wordlist-ordered woke vet lint ## Runs all the checks on the project.

##@ Documentation

licenses: go-licenses ## Generate the licenses folder.
	# The following statement is expected to fail because our license is unrecognised
	$(GO_LICENSES) \
		save github.com/cloudnative-pg/cloudnative-pg \
		--save_path licenses/go-licenses --force || true
	chmod a+rw -R licenses/go-licenses
	find licenses/go-licenses \( -name '*.mod' -or -name '*.go' \) -delete

apidoc: k8s-api-docgen ## Update the API Reference section of the documentation.
	set -e ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	echo $$CONFIG_TMP_DIR ;\
	$(K8S_API_DOCGEN) -t md \
	  -c docs/k8s-api-docgen.yaml \
	  -m docs/src/api_reference.md.in \
	  -o $${CONFIG_TMP_DIR}/api_reference.md \
	  api/v1/*_types.go ;\
	cp $${CONFIG_TMP_DIR}/api_reference.md docs/src/api_reference.md

##@ Tools

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

KUSTOMIZE = $(LOCALBIN)/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@$(KUSTOMIZE_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

K8S_API_DOCGEN = $(LOCALBIN)/k8s-api-docgen
k8s-api-docgen: ## Download k8s-api-docgen locally if necessary.
	$(call go-install-tool,$(K8S_API_DOCGEN),github.com/EnterpriseDB/k8s-api-docgen/cmd/k8s-api-docgen@latest)

GO_LICENSES = $(LOCALBIN)/go-licenses
go-licenses: ## Download go-licenses locally if necessary.
	$(call go-install-tool,$(GO_LICENSES),github.com/google/go-licenses@latest)

GO_RELEASER = $(LOCALBIN)/goreleaser
go-releaser: ## Download go-releaser locally if necessary.
	$(call go-install-tool,$(GO_RELEASER),github.com/goreleaser/goreleaser@$(GORELEASER_VERSION))

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
# go-install-tool will 'go install' any package $2 and install it to $1.
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
}
endef

kind-cluster: ## Create KinD cluster to run operator locally
	set -e ;\
	hack/setup-cluster.sh -n1 -r create

kind-cluster-destroy: ## Destroy KinD cluster created using kind-cluster command
	set -e ;\
	hack/setup-cluster.sh -n1 -r destroy
