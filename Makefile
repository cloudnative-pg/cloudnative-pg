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
BUNDLE_IMG = ghcr.io/cloudnative-pg/cloudnative-pg-testing:bundle-${IMAGE_TAG}
endif
endif

COMMIT := $(shell git rev-parse --short HEAD || echo unknown)
DATE := $(shell git log -1 --pretty=format:'%ad' --date short)
VERSION := $(shell git describe --tags --match 'v*' | sed -e 's/^v//; s/-g[0-9a-f]\+$$//; s/-\([0-9]\+\)$$/-dev\1/')
LDFLAGS= "-X github.com/cloudnative-pg/cloudnative-pg/pkg/versions.buildVersion=${VERSION} $\
-X github.com/cloudnative-pg/cloudnative-pg/pkg/versions.buildCommit=${COMMIT} $\
-X github.com/cloudnative-pg/cloudnative-pg/pkg/versions.buildDate=${DATE}"
DIST_PATH := $(shell pwd)/dist
OPERATOR_MANIFEST_PATH := ${DIST_PATH}/operator-manifest.yaml
LOCALBIN ?= $(shell pwd)/bin

BUILD_IMAGE ?= true
POSTGRES_IMAGE_NAME ?= $(shell grep 'DefaultImageName.*=' "pkg/versions/versions.go" | cut -f 2 -d \")
KUSTOMIZE_VERSION ?= v5.1.1
KIND_CLUSTER_NAME ?= pg
KIND_CLUSTER_VERSION ?= v1.28.0
CONTROLLER_TOOLS_VERSION ?= v0.13.0
GORELEASER_VERSION ?= v1.21.2
SPELLCHECK_VERSION ?= 0.34.0
WOKE_VERSION ?= 0.19.0
OPERATOR_SDK_VERSION ?= 1.31.0
ARCH ?= amd64

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
	export KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT=60s ;\
	export KUBEBUILDER_CONTROLPLANE_START_TIMEOUT=60s ;\
	go test -coverpkg=./... --count=1 -coverprofile=cover.out ./api/... ./cmd/... ./controllers/... ./internal/... ./pkg/... ./tests/utils ;

e2e-test-kind: ## Run e2e tests locally using kind.
	hack/e2e/run-e2e-kind.sh

e2e-test-k3d: ## Run e2e tests locally using k3d.
	hack/e2e/run-e2e-k3d.sh

e2e-test-local: ## Run e2e tests locally using the default kubernetes context.
	hack/e2e/run-e2e-local.sh

##@ Build
build: generate fmt vet build-manager build-plugin ## Build binaries.

build-manager: generate fmt vet ## Build manager binary.
	go build -o bin/manager -ldflags ${LDFLAGS} ./cmd/manager

build-plugin: generate fmt vet ## Build plugin binary.
	go build -o bin/kubectl-cnpg -ldflags ${LDFLAGS} ./cmd/kubectl-cnpg

run: generate fmt vet manifests ## Run against the configured Kubernetes cluster in ~/.kube/config.
	go run ./cmd/manager

docker-build: go-releaser ## Build the docker image.
	GOOS=linux GOARCH=${ARCH} GOPATH=$(go env GOPATH) DATE=${DATE} COMMIT=${COMMIT} VERSION=${VERSION} \
	  $(GO_RELEASER) build --skip-validate --clean --single-target
	DOCKER_BUILDKIT=1 docker build . -t ${CONTROLLER_IMG} --build-arg VERSION=${VERSION}

docker-push: ## Push the docker image.
	docker push ${CONTROLLER_IMG}

olm-bundle: manifests kustomize operator-sdk ## Build the bundle for OLM installation
	set -xeEuo pipefail ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	cp -r config "$${CONFIG_TMP_DIR}" ;\
	( \
		cd "$${CONFIG_TMP_DIR}/config/default" ;\
		$(KUSTOMIZE) edit set image controller="$${CONTROLLER_IMG}" ;\
		cd "$${CONFIG_TMP_DIR}" ;\
	) ;\
	rm -fr bundle bundle.Dockerfile ;\
	($(KUSTOMIZE) build "$${CONFIG_TMP_DIR}/config/olm-manifests") | \
	sed -e "s@\$${CREATED_AT}@$$(LANG=C date -Iseconds -u)@g" | \
	$(OPERATOR_SDK) generate bundle --verbose --overwrite --manifests --metadata --package cloudnative-pg --channels stable-v1 --use-image-digests --default-channel stable-v1 --version "${VERSION}" ; \
	docker buildx build --no-cache -f bundle.Dockerfile --push -t ${BUNDLE_IMG} . ;\
	export BUNDLE_IMG="${BUNDLE_IMG}"

olm-catalog: olm-bundle opm ## Build and push the index image for OLM Catalog
	set -xeEuo pipefail ;\
	rm -fr catalog* cloudnative-pg-operator-template.yaml ;\
	mkdir -p catalog/cloudnative-pg ;\
	$(OPM) generate dockerfile catalog
	echo -e "Schema: olm.semver\n\
	GenerateMajorChannels: true\n\
	GenerateMinorChannels: false\n\
	Stable:\n\
	    Bundles:\n\
	    - Image: ${BUNDLE_IMG}" | envsubst > cloudnative-pg-operator-template.yaml
	$(OPM) alpha render-template semver -o yaml < cloudnative-pg-operator-template.yaml > catalog/catalog.yaml ;\
	$(OPM) validate catalog/ ;\
	DOCKER_BUILDKIT=1 docker buildx build --push -f catalog.Dockerfile -t ghcr.io/cloudnative-pg/cloudnative-pg-testing:catalog-${VERSION} --push . ;\
	echo -e "apiVersion: operators.coreos.com/v1alpha1\n\
	kind: CatalogSource\n\
	metadata:\n\
	   name: cloudnative-pg-catalog\n\
	   namespace: operators\n\
	spec:\n\
	   sourceType: grpc\n\
	   image: ghcr.io/cloudnative-pg/cloudnative-pg-testing:catalog-${VERSION}" | envsubst > cloudnative-pg-catalog.yaml ;\

##@ Deployment
install: manifests kustomize ## Install CRDs into a cluster.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

uninstall: manifests kustomize ## Uninstall CRDs from a cluster.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: generate-manifest ## Deploy controller in the configured Kubernetes cluster in ~/.kube/config.
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
	mkdir -p ${DIST_PATH} ;\
	$(KUSTOMIZE) build $$CONFIG_TMP_DIR/default > ${OPERATOR_MANIFEST_PATH} ;\
	rm -fr $$CONFIG_TMP_DIR

manifests: controller-gen ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

deploy-locally: kind-cluster ## Build and deploy operator in local cluster
	set -e ;\
	hack/setup-cluster.sh -n1 -r load deploy

olm-scorecard: operator-sdk ## Run the Scorecard test from operator-sdk
	$(OPERATOR_SDK) scorecard ${BUNDLE_IMG} --wait-time 60s --verbose

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
	docker run --rm -v $(PWD):/tmp jonasbn/github-action-spellcheck:$(SPELLCHECK_VERSION)

woke: ## Runs the woke checks on project.
	docker run --rm -v $(PWD):/src -w /src getwoke/woke:$(WOKE_VERSION) woke -c .woke.yaml

wordlist-ordered: ## Order the wordlist using sort
	LANG=C LC_ALL=C sort .wordlist-en-custom.txt > .wordlist-en-custom.txt.new && \
	mv -f .wordlist-en-custom.txt.new .wordlist-en-custom.txt

go-mod-check: ## Check if there's any dirty change after `go mod tidy`
	go mod tidy ;\
	git diff --exit-code go.mod go.sum

checks: go-mod-check generate manifests apidoc fmt spellcheck wordlist-ordered woke vet lint ## Runs all the checks on the project.

##@ Documentation

licenses: go-licenses ## Generate the licenses folder.
	# The following statement is expected to fail because our license is unrecognised
	$(GO_LICENSES) \
		save github.com/cloudnative-pg/cloudnative-pg \
		--save_path licenses/go-licenses --force || true
	chmod a+rw -R licenses/go-licenses
	find licenses/go-licenses \( -name '*.mod' -or -name '*.go' \) -delete

apidoc: genref ## Update the API Reference section of the documentation.
	cd ./docs && \
	$(GENREF) -c config.yaml \
      -include cloudnative-pg \
      -o src

##@ Cleanup

clean: ## Clean-up the work tree from build/test artifacts
	rm -rf $(LOCALBIN)/kubectl-cnpg $(LOCALBIN)/manager $(DIST_PATH) _*/ tests/e2e/out/ cover.out

distclean: clean ## Clean-up the work tree removing also cached tools binaries
	! [ -d "$(ENVTEST_ASSETS_DIR)" ] || chmod -R u+w $(ENVTEST_ASSETS_DIR)
	rm -rf $(LOCALBIN) $(ENVTEST_ASSETS_DIR)

##@ Tools

## Location to install dependencies to
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
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION))

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

GENREF = $(LOCALBIN)/genref
genref: ## Download kubernetes-sigs/reference-docs/genref locally if necessary.
	$(call go-install-tool,$(GENREF),github.com/kubernetes-sigs/reference-docs/genref@master) # wokeignore:rule=master

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

.PHONY: operator-sdk
operator-sdk: ## Install the operator-sdk app
ifneq ($(shell PATH="$(GOBIN):$${PATH}" operator-sdk version | awk -F '"' '{print $$2}'), $(OPERATOR_SDK_VERSION))
	@{ \
	set -e ;\
	mkdir -p $(GOBIN) ;\
	GO_ARCH=$(shell go env GOARCH) ;\
	SDK_OS="linux" ;\
	if [ $$(uname) = "Darwin" ]; then SDK_OS="darwin"; fi ;\
	curl -s -L "https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk_$${SDK_OS}_$${GO_ARCH}" -o "$(GOBIN)/operator-sdk" ;\
	chmod +x "$(GOBIN)/operator-sdk" ;\
	}
OPERATOR_SDK=$(GOBIN)/operator-sdk
else
OPERATOR_SDK=$(shell which operator-sdk)
endif

.PHONY: opm
opm: ## Download opm locally if necessary.
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	OPM_VERSION=$$(curl -s -LH "Accept:application/json" https://github.com/operator-framework/operator-registry/releases/latest | sed 's/.*"tag_name":"\([^"]\+\)".*/\1/') ;\
	curl -sSL https://github.com/operator-framework/operator-registry/releases/download/$${OPM_VERSION}/$${OS}-$${ARCH}-opm -o "$(GOBIN)/opm";\
	chmod +x $(GOBIN)/opm ;\
	}
OPM=$(GOBIN)/opm
else
OPM=$(shell which opm)
endif
