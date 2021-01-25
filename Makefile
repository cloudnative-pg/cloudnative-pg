# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2021 EnterpriseDB Corporation.

# Image URL to use all building/pushing image targets
CONTROLLER_IMG ?= quay.io/enterprisedb/cloud-native-postgresql-testing:$(shell (git symbolic-ref -q --short HEAD || git describe --tags --exact-match) | tr / -)
BUILD_IMAGE ?= true
POSTGRES_IMAGE_NAME ?= quay.io/enterprisedb/postgresql:13
KUSTOMIZE_VERSION ?= v3.5.4
KIND_CLUSTER_NAME ?= pg
KIND_CLUSTER_VERSION ?= v1.20.0

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
	go test ./api/... ./cmd/... ./controllers/... ./pkg... -coverprofile cover.out

# Run e2e tests locally using kind
e2e-test-kind:
	hack/e2e/run-e2e-kind.sh

e2e-test-k3d:
	hack/e2e/run-e2e-k3d.sh

# Build binaries
build: generate fmt vet
	go build -o bin/manager ./cmd/manager
	go build -o bin/kubectl-cnp ./cmd/kubectl-cnp

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
	    $(KUSTOMIZE) edit add patch manager_image_pull_secret.yaml ;\
	    cd $$CONFIG_TMP_DIR/manager ;\
	    $(KUSTOMIZE) edit set image controller=${CONTROLLER_IMG} ;\
	    $(KUSTOMIZE) edit add patch env_override.yaml ;\
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
docker-build: test
	docker build . -t ${CONTROLLER_IMG}

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
apidoc: po-docgen
	set -e ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	echo $$CONFIG_TMP_DIR ;\
	$(PO_DOCGEN) api api/v1alpha1/*_types.go | sed 's/\\n/\n/g' | \
	  sed -n '/## Table of Contents/,$$p' | \
	  sed 's/^## Table of Contents/<!-- TOC -->/' | \
	  grep -v '#table-of-contents' > $${CONFIG_TMP_DIR}/api_reference.new.md ;\
	sed '/<!-- TOC -->/,$${/<!-- TOC -->/!d;}' \
	  docs/src/api_reference.md > $${CONFIG_TMP_DIR}/api_reference.md ;\
	sed 1d \
	  $${CONFIG_TMP_DIR}/api_reference.new.md >> $${CONFIG_TMP_DIR}/api_reference.md ;\
	sed -i 's/| ----- | ----------- | ------ | -------- |/| -------------------- | ------------------------------ | -------------------- | -------- |/' $${CONFIG_TMP_DIR}/api_reference.md ;\
	cp $${CONFIG_TMP_DIR}/api_reference.md docs/src/api_reference.md

# find or download controller-gen
.PHONY: controller-gen
controller-gen:
# download controller-gen if necessary
ifneq ($(shell controller-gen --version), Version: v0.4.1)
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# find or download go-licenses
.PHONY: go-licenses
go-licenses:
# download go-licenses if necessary
ifeq (, $(shell which go-licenses))
	@{ \
	set -e ;\
	GO_LICENSES_TMP_DIR=$$(mktemp -d) ;\
	cd $$GO_LICENSES_TMP_DIR ;\
	go mod init tmp ;\
	go get github.com/google/go-licenses ;\
	rm -rf $$GO_LICENSES_TMP_DIR ;\
	}
GO_LICENSES=$(GOBIN)/go-licenses
else
GO_LICENSES=$(shell which go-licenses)
endif

# find or download po-docgen
.PHONY: po-docgen
po-docgen:
# download po-docgen if necessary
ifeq (, $(shell which po-docgen))
	@{ \
	set -e ;\
	PO_DOCGEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$PO_DOCGEN_TMP_DIR ;\
	go mod init tmp ;\
	go get github.com/prometheus-operator/prometheus-operator/cmd/po-docgen@v0.43.0 ;\
	rm -rf $$PO_DOCGEN_TMP_DIR ;\
	}
PO_DOCGEN=$(GOBIN)/po-docgen
else
PO_DOCGEN=$(shell which po-docgen)
endif

# find or download kustomize
.PHONY: kustomize
kustomize:
ifneq ($(shell PATH="$(GOBIN):$${PATH}" kustomize version --short | awk -F '[/ ]' '{print $$2}'), $(KUSTOMIZE_VERSION))
	@{ \
	set -e ;\
	curl -sSfL https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_$$(uname | tr '[:upper:]' '[:lower:]')_amd64.tar.gz | \
	tar -xz -C ${GOBIN} ;\
	}
KUSTOMIZE=$(GOBIN)/kustomize
else
KUSTOMIZE=$(shell PATH="$(GOBIN):$${PATH}" which kustomize)
endif

# initialize a local development cluster using kind
dev-init:
	kind create cluster --name=$(KIND_CLUSTER_NAME) --image kindest/node:$(KIND_CLUSTER_VERSION)
	$(MAKE) deploy
	kubectl create secret docker-registry \
	  postgresql-operator-pull-secret \
	  -n postgresql-operator-system \
	  --docker-username="${QUAY_USERNAME}" \
	  --docker-password="${QUAY_PASSWORD}" \
	  --docker-server="quay.io/enterprisedb"
	echo
	while [[ $$( kubectl get pods -n postgresql-operator-system -l control-plane=controller-manager -o 'jsonpath={..status.conditions[?(@.type=="Ready")].status}' ) != "True" ]]; do \
	    printf '\033[0K\r'; \
	    kubectl get pods -n postgresql-operator-system -l control-plane=controller-manager \
	        -o 'jsonpath=Waiting for pod: {..status.phase} {..status.containerStatuses[*].state.waiting.reason}' ; \
	    sleep 2 ; \
	done
	echo
	kubectl apply -f docs/src/samples/cluster-example.yaml

# clean up the kind cluster created by dev-init command
dev-clean:
	kind delete cluster --name=$(KIND_CLUSTER_NAME)

# reinitialize the local kind cluster
dev-reset: dev-clean dev-init
