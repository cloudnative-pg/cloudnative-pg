#
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
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
# SPDX-License-Identifier: Apache-2.0
#

# Image URL to use all building/pushing image targets
IMAGE_NAME ?= ghcr.io/cloudnative-pg/cloudnative-pg-testing

# Prevent e2e tests to proceed with empty tag which
# will be considered as "latest".
ifeq (,$(CONTROLLER_IMG))
IMAGE_TAG = $(shell (git symbolic-ref -q --short HEAD || git describe --tags --exact-match) | tr / -)
ifneq (,${IMAGE_TAG})
CONTROLLER_IMG = ${IMAGE_NAME}:${IMAGE_TAG}
endif
endif
CATALOG_IMG ?= ${CONTROLLER_IMG}-catalog
BUNDLE_IMG ?= ${CONTROLLER_IMG}-bundle
INDEX_IMG ?= ${CONTROLLER_IMG}-index

# Define CONTROLLER_IMG_WITH_DIGEST by appending CONTROLLER_IMG_SHA to CONTROLLER_IMG with '@' if CONTROLLER_IMG_SHA is set
ifneq ($(CONTROLLER_IMG_DIGEST),)
CONTROLLER_IMG_WITH_DIGEST := $(CONTROLLER_IMG)@$(CONTROLLER_IMG_DIGEST)
else
CONTROLLER_IMG_WITH_DIGEST := $(CONTROLLER_IMG)
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
PGBOUNCER_IMAGE_NAME ?= $(shell grep 'DefaultPgbouncerImage.*=' "pkg/versions/versions.go" | cut -f 2 -d \")
# renovate: datasource=github-releases depName=kubernetes-sigs/kustomize versioning=loose
KUSTOMIZE_VERSION ?= v5.6.0
# renovate: datasource=go depName=sigs.k8s.io/controller-tools
CONTROLLER_TOOLS_VERSION ?= v0.20.0
# renovate: datasource=go depName=github.com/elastic/crd-ref-docs
CRDREFDOCS_VERSION ?= v0.2.0
# renovate: datasource=go depName=github.com/goreleaser/goreleaser
GORELEASER_VERSION ?= v2.13.2
# renovate: datasource=docker depName=jonasbn/github-action-spellcheck versioning=docker
SPELLCHECK_VERSION ?= 0.56.0
# renovate: datasource=docker depName=getwoke/woke versioning=docker
WOKE_VERSION ?= 0.19.0
# renovate: datasource=github-releases depName=operator-framework/operator-sdk versioning=loose
OPERATOR_SDK_VERSION ?= v1.42.0
# renovate: datasource=github-tags depName=operator-framework/operator-registry
OPM_VERSION ?= v1.61.0
# renovate: datasource=github-tags depName=redhat-openshift-ecosystem/openshift-preflight
PREFLIGHT_VERSION ?= 1.16.0
ARCH ?= amd64

export CONTROLLER_IMG
export BUILD_IMAGE
export POSTGRES_IMAGE_NAME
export PGBOUNCER_IMAGE_NAME
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

print-version:
	echo ${VERSION}

ENVTEST_ASSETS_DIR=$$(pwd)/testbin
test: generate fmt vet manifests envtest ## Run tests.
	mkdir -p ${ENVTEST_ASSETS_DIR} ;\
	source <(${ENVTEST} use -p env --bin-dir ${ENVTEST_ASSETS_DIR} ${ENVTEST_K8S_VERSION}) ;\
	export KUBEBUILDER_CONTROLPLANE_STOP_TIMEOUT=60s ;\
	export KUBEBUILDER_CONTROLPLANE_START_TIMEOUT=60s ;\
	go test -coverpkg=./... -coverprofile=cover.out ./api/... ./cmd/... ./internal/... ./pkg/... ./tests/utils/...

test-race: generate fmt vet manifests envtest ## Run tests enabling race detection.
	mkdir -p ${ENVTEST_ASSETS_DIR} ;\
	source <(${ENVTEST} use -p env --bin-dir ${ENVTEST_ASSETS_DIR} ${ENVTEST_K8S_VERSION}) ;\
	go run github.com/onsi/ginkgo/v2/ginkgo -r -p --skip-package=e2e \
	  --race --keep-going --fail-on-empty --randomize-all --randomize-suites

e2e-test-kind: ## Run e2e tests locally using kind.
	hack/e2e/run-e2e-kind.sh

e2e-test-local: ## Run e2e tests locally using the default kubernetes context.
	hack/e2e/run-e2e-local.sh

##@ Build
build: generate fmt vet build-manager build-plugin ## Build binaries.

build-manager: generate fmt vet ## Build manager binary.
	go build -o bin/manager -ldflags ${LDFLAGS} ./cmd/manager

build-plugin: generate fmt vet ## Build plugin binary.
	go build -o bin/kubectl-cnpg -ldflags ${LDFLAGS} ./cmd/kubectl-cnpg

build-race: generate fmt vet build-manager-race build-plugin-race ## Build the binaries adding the -race option.

build-manager-race: generate fmt vet ## Build manager binary with -race option.
	go build -race -o bin/manager -ldflags ${LDFLAGS} ./cmd/manager

build-plugin-race: generate fmt vet ## Build plugin binary.
	go build -race -o bin/kubectl-cnpg -ldflags ${LDFLAGS} ./cmd/kubectl-cnpg


run: generate fmt vet manifests ## Run against the configured Kubernetes cluster in ~/.kube/config.
	go run ./cmd/manager

docker-build: go-releaser ## Build the docker image.
	GOOS=linux GOARCH=${ARCH} GOPATH=$(go env GOPATH) DATE=${DATE} COMMIT=${COMMIT} VERSION=${VERSION} \
	  $(GO_RELEASER) build --skip=validate --clean --single-target $(if $(VERSION),,--snapshot); \
	builder_name_option=""; \
	if [ -n "${BUILDER_NAME}" ]; then \
	  builder_name_option="--builder ${BUILDER_NAME}"; \
	fi; \
	DOCKER_BUILDKIT=1 buildVersion=${VERSION} revision=${COMMIT} \
	  docker buildx bake $${builder_name_option} --set=*.platform="linux/${ARCH}" \
	  --set distroless.tags="$${CONTROLLER_IMG}" \
	  --push distroless

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
	sed -i -e "s/ClusterRole/Role/" "$${CONFIG_TMP_DIR}/config/rbac/role.yaml" "$${CONFIG_TMP_DIR}/config/rbac/role_binding.yaml"  ;\
	($(KUSTOMIZE) build "$${CONFIG_TMP_DIR}/config/olm-manifests") | \
	$(OPERATOR_SDK) generate bundle --verbose --overwrite --manifests --metadata --package cloudnative-pg --channels stable-v1 --use-image-digests --default-channel stable-v1 --version "${VERSION}" ; \
	DOCKER_BUILDKIT=1 docker build --push --no-cache -f bundle.Dockerfile -t ${BUNDLE_IMG} . ;\
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
	$(OPM) index add --mode semver --container-tool docker --bundles "${BUNDLE_IMG}" --tag "${INDEX_IMG}" ;\
	docker push ${INDEX_IMG} ;\
	DOCKER_BUILDKIT=1 docker build --push -f catalog.Dockerfile -t ${CATALOG_IMG} . ;\
	echo -e "apiVersion: operators.coreos.com/v1alpha1\n\
	kind: CatalogSource\n\
	metadata:\n\
	   name: cloudnative-pg-catalog\n\
	   namespace: operators\n\
	spec:\n\
	   sourceType: grpc\n\
	   image: ${CATALOG_IMG}\n\
	   secrets:\n\
       - cnpg-pull-secret" | envsubst > cloudnative-pg-catalog.yaml ;\

##@ Deployment
install: manifests kustomize ## Install CRDs into a cluster.
	$(KUSTOMIZE) build config/crd | kubectl apply --server-side -f -

uninstall: manifests kustomize ## Uninstall CRDs from a cluster.
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

deploy: generate-manifest ## Deploy controller in the configured Kubernetes cluster in ~/.kube/config.
	kubectl apply --server-side --force-conflicts -f ${OPERATOR_MANIFEST_PATH}

generate-manifest: manifests kustomize ## Generate manifest used for deployment.
	set -e ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	cp -r config/* $$CONFIG_TMP_DIR ;\
	{ \
		cd $$CONFIG_TMP_DIR/default ;\
		$(KUSTOMIZE) edit add patch --path manager_image_pull_secret.yaml ;\
		cd $$CONFIG_TMP_DIR/manager ;\
		$(KUSTOMIZE) edit set image controller="${CONTROLLER_IMG_WITH_DIGEST}" ;\
		$(KUSTOMIZE) edit add patch --path env_override.yaml ;\
		$(KUSTOMIZE) edit add configmap controller-manager-env \
			--from-literal="POSTGRES_IMAGE_NAME=${POSTGRES_IMAGE_NAME}" \
			--from-literal="PGBOUNCER_IMAGE_NAME=${PGBOUNCER_IMAGE_NAME}" ;\
	} ;\
	mkdir -p ${DIST_PATH} ;\
	$(KUSTOMIZE) build $$CONFIG_TMP_DIR/default > ${OPERATOR_MANIFEST_PATH} ;\
	rm -fr $$CONFIG_TMP_DIR

manifests: controller-gen ## Generate manifests e.g. CRD, RBAC etc.
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

olm-scorecard: operator-sdk ## Run the Scorecard test from operator-sdk
	$(OPERATOR_SDK) scorecard ${BUNDLE_IMG} --wait-time 60s --verbose

##@ Formatters and Linters

fmt: ## Run go fmt against code.
	go fmt ./...

vet: ## Run go vet against code.
	go vet ./...

lint: ## Run the linter.
	golangci-lint run

lint-fix: ## Run the linter with --fix.
	golangci-lint run --fix

shellcheck: ## Shellcheck for the hack directory.
	@{ \
	set -e ;\
	find -name '*.sh' -exec shellcheck -a -S style {} + ;\
	}

spellcheck: ## Runs the spellcheck on the project.
	docker run --rm -v $(PWD):/tmp:Z jonasbn/github-action-spellcheck:$(SPELLCHECK_VERSION)

woke: ## Runs the woke checks on project.
	docker run --rm -v $(PWD):/src:Z -w /src getwoke/woke:$(WOKE_VERSION) woke -c .woke.yaml

wordlist-ordered: ## Order the wordlist using sort
	LANG=C LC_ALL=C sort .wordlist-en-custom.txt > .wordlist-en-custom.txt.new && \
	mv -f .wordlist-en-custom.txt.new .wordlist-en-custom.txt

go-mod-check: ## Check if there's any dirty change after `go mod tidy`
	go mod tidy ;\
	git diff --exit-code go.mod go.sum

run-govulncheck: govulncheck ## Check if there's any known vulnerabilities with the currently installed Go modules
	$(GOVULNCHECK) ./...

checks: go-mod-check generate manifests apidoc fmt spellcheck wordlist-ordered woke vet lint run-govulncheck ## Runs all the checks on the project.

##@ Documentation

licenses: go-licenses ## Generate the licenses folder.
	# The following statement is expected to fail because our license is unrecognised
	$(GO_LICENSES) \
		save ./... \
		--save_path licenses/go-licenses --force || true
	chmod a+rw -R licenses/go-licenses
	find licenses/go-licenses \( -name '*.mod' -or -name '*.go' \) -delete

apidoc: crd-ref-docs ## Update the API Reference section of the documentation.
	$(CRDREFDOCS) --source-path api/v1 \
		--config docs/crd-gen-refs/config.yaml \
		--renderer markdown \
		--max-depth 15 \
		--templates-dir docs/crd-gen-refs/markdown \
		--output-path docs/src/cloudnative-pg.v1.md

##@ Cleanup

clean: ## Clean-up the work tree from build/test artifacts
	rm -rf $(LOCALBIN)/kubectl-cnpg $(LOCALBIN)/manager $(DIST_PATH) _*/ tests/e2e/out/ tests/e2e/*_logs/ cover.out

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

CRDREFDOCS = $(LOCALBIN)/crd-ref-docs
crd-ref-docs: ## Download github.com/elastic/crd-ref-docs locally if necessary.
	$(call go-install-tool,$(CRDREFDOCS),github.com/elastic/crd-ref-docs@$(CRDREFDOCS_VERSION))

GO_LICENSES = $(LOCALBIN)/go-licenses
go-licenses: ## Download go-licenses locally if necessary.
	$(call go-install-tool,$(GO_LICENSES),github.com/google/go-licenses@latest)

GO_RELEASER = $(LOCALBIN)/goreleaser
go-releaser: ## Download go-releaser locally if necessary.
	$(call go-install-tool,$(GO_RELEASER),github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION))

.PHONY: govulncheck
GOVULNCHECK = $(LOCALBIN)/govulncheck
govulncheck: ## Download govulncheck locally if necessary.
	$(call go-install-tool,$(GOVULNCHECK),golang.org/x/vuln/cmd/govulncheck@latest)

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
# go-install-tool will 'go install' any package $2 and install it to $1.
define go-install-tool
@[ -f $(1) ] || { \
set -e ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go install $(2) ;\
}
endef

.PHONY: operator-sdk
OPERATOR_SDK = $(LOCALBIN)/operator-sdk
operator-sdk: ## Install the operator-sdk app
ifneq ($(shell $(OPERATOR_SDK) version 2>/dev/null | awk -F '"' '{print $$2}'), $(OPERATOR_SDK_VERSION))
	@{ \
	set -e ;\
	mkdir -p $(LOCALBIN) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSL "https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/operator-sdk_$${OS}_$${ARCH}" -o "$(OPERATOR_SDK)" ;\
	chmod +x "$(LOCALBIN)/operator-sdk" ;\
	}
endif

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifneq ($(shell $(OPM) version 2>/dev/null | awk -F '"' '{print $$2}'), $(OPM_VERSION))
	@{ \
	set -e ;\
	mkdir -p $(LOCALBIN) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSL https://github.com/operator-framework/operator-registry/releases/download/${OPM_VERSION}/$${OS}-$${ARCH}-opm -o "$(OPM)";\
	chmod +x $(LOCALBIN)/opm ;\
	}
endif

.PHONY: preflight
PREFLIGHT = $(LOCALBIN)/preflight
preflight: ## Download preflight locally if necessary.
ifneq ($(shell $(PREFLIGHT) --version 2>/dev/null | awk '{print $$3}'), $(PREFLIGHT_VERSION))
	@{ \
	set -e ;\
	mkdir -p $(LOCALBIN) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSL "https://github.com/redhat-openshift-ecosystem/openshift-preflight/releases/download/${PREFLIGHT_VERSION}/preflight-$${OS}-$${ARCH}" -o "$(PREFLIGHT)" ;\
	chmod +x $(LOCALBIN)/preflight ;\
	}
endif
