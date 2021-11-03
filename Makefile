# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2021 EnterpriseDB Corporation.

# Image URL to use all building/pushing image targets

# Prevent e2e tests to proceed with empty tag which
# will be considered as "latest" (#CNP-289).
ifeq (,$(CONTROLLER_IMG))
IMAGE_TAG = $(shell (git symbolic-ref -q --short HEAD || git describe --tags --exact-match) | tr / -)
ifneq (,${IMAGE_TAG})
CONTROLLER_IMG = quay.io/enterprisedb/cloud-native-postgresql-testing:${IMAGE_TAG}
endif
endif

COMMIT := $(shell git rev-parse --short HEAD || echo unknown)
DATE := $(shell git log -1 --pretty=format:'%ad' --date short)
VERSION := $(shell git describe --tags --match 'v*' | sed -e 's/^v//; s/-g[0-9a-f]\+$$//; s/-\([0-9]\+\)$$/+dev\1/')
LDFLAGS= "-X github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions.buildVersion=${VERSION} $\
-X github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions.buildCommit=${COMMIT} $\
-X github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions.buildDate=${DATE}"

BUILD_IMAGE ?= true
POSTGRES_IMAGE_NAME ?= quay.io/enterprisedb/postgresql:13
KUSTOMIZE_VERSION ?= v4.3.0
KIND_CLUSTER_NAME ?= pg
KIND_CLUSTER_VERSION ?= v1.22.2

export CONTROLLER_IMG
export BUILD_IMAGE
export POSTGRES_IMAGE_NAME

# We don't need `trivialVersions=true` anymore, with `crd` it's ok for multi versions
CRD_OPTIONS ?= "crd"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: build

# Run tests
test: generate fmt vet manifests
	go test ./api/... ./cmd/... ./controllers/... ./internal/... ./pkg/... -coverprofile cover.out

# Run e2e tests locally using kind
e2e-test-kind:
	hack/e2e/run-e2e-kind.sh

e2e-test-k3d:
	hack/e2e/run-e2e-k3d.sh

# Build binaries
build: generate fmt vet
	go build -o bin/manager -ldflags ${LDFLAGS} ./cmd/manager
	go build -o bin/kubectl-cnp -ldflags ${LDFLAGS} ./cmd/kubectl-cnp

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./cmd/manager

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize
	set -e ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	cp -r config/* $$CONFIG_TMP_DIR ;\
	{ \
	    cd $$CONFIG_TMP_DIR/default ;\
	    $(KUSTOMIZE) edit add patch --path manager_image_pull_secret.yaml ;\
	    cd $$CONFIG_TMP_DIR/manager ;\
	    $(KUSTOMIZE) edit set image controller=${CONTROLLER_IMG} ;\
	    $(KUSTOMIZE) edit add patch --path env_override.yaml ;\
	    $(KUSTOMIZE) edit add configmap controller-manager-env \
	        --from-literal=POSTGRES_IMAGE_NAME=${POSTGRES_IMAGE_NAME} ;\
	} ;\
	$(KUSTOMIZE) build $$CONFIG_TMP_DIR/default | kubectl apply -f - ;\
	rm -fr $$CONFIG_TMP_DIR

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Run the linter
lint:
	golangci-lint run

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build the docker image
docker-build: go-releaser
	GOOS=linux GOARCH=amd64 DATE=${DATE} COMMIT=${COMMIT} VERSION=${VERSION} \
	  $(GO_RELEASER) build -f .goreleaser-multiarch.yml --skip-validate --rm-dist --single-target
	DOCKER_BUILDKIT=1 docker build . -t ${CONTROLLER_IMG} --build-arg VERSION=${VERSION}

# Push the docker image
docker-push:
	docker push ${CONTROLLER_IMG}

# Generate the licenses folder
.PHONY: licenses
licenses: go-licenses
	# The following statement is expected to fail because our license is unrecognised
	GOPRIVATE="https://github.com/EnterpriseDB/*" $(GO_LICENSES) \
		save github.com/EnterpriseDB/cloud-native-postgresql \
		--save_path licenses/go-licenses --force || true
	chmod a+rw -R licenses/go-licenses
	find licenses/go-licenses \( -name '*.mod' -or -name '*.go' \) -delete

# Update the API Reference section of the documentation
apidoc: k8s-api-docgen
	set -e ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	echo $$CONFIG_TMP_DIR ;\
	$(K8S_API_DOCGEN) -t md \
	  -c docs/k8s-api-docgen.yaml \
	  -m docs/src/api_reference.md.in \
	  -o $${CONFIG_TMP_DIR}/api_reference.md \
	  api/v1/*_types.go ;\
	cp $${CONFIG_TMP_DIR}/api_reference.md docs/src/api_reference.md

# Shellcheck for the hack directory
shellcheck:
	@{ \
	set -e ;\
	find -name '*.sh' -exec shellcheck -a -S style {} + ;\
	}

CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.7.0)

KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v4@$(KUSTOMIZE_VERSION))

K8S_API_DOCGEN = $(shell pwd)/bin/k8s-api-docgen
k8s-api-docgen: ## Download k8s-api-docgen locally if necessary.
	$(call go-get-tool,$(K8S_API_DOCGEN),github.com/EnterpriseDB/k8s-api-docgen/cmd/k8s-api-docgen@v0.0.0-20210520152405-ab1617492c9f)

GO_LICENSES = $(shell pwd)/bin/go-licenses
go-licenses: ## Download go-licenses locally if necessary.
	$(call go-get-tool,$(GO_LICENSES),github.com/google/go-licenses)

GO_RELEASER = $(shell pwd)/bin/goreleaser
go-releaser: ## Download go-releaser locally if necessary.
	$(call go-get-tool,$(GO_RELEASER),github.com/goreleaser/goreleaser)


# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp 2>/dev/null;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

spellcheck:
	docker run --rm -v $(PWD):/tmp jonasbn/github-action-spellcheck:0.14.0

woke:
	docker run --rm -v $(PWD):/src -w /src getwoke/woke:0.9 woke -c .woke.yaml

checks: generate manifests apidoc fmt spellcheck woke vet lint
