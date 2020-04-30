# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.

# Image URL to use all building/pushing image targets
CONTROLLER_IMG ?= quay.io/2ndquadrant/cloud-native-postgresql-operator:latest
BUILD_IMAGE ?= true
POSTGRES_IMAGE_NAME ?= quay.io/2ndquadrant/postgres:latest
export CONTROLLER_IMG BUILD_IMAGE POSTGRES_IMAGE_NAME

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

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

# Run e2e tests
e2e-test:
	hack/e2e/run-e2e.sh

# Build binaries
build: generate fmt vet
	go build -o bin/manager ./cmd/manager
	go build -o bin/kubectl-cnp ./cmd/kubectl-cnp

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./cmd/manager

# Install CRDs into a cluster
install: manifests
	kustomize build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests
	kustomize build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	set -e ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	cp -r config/* $$CONFIG_TMP_DIR ;\
	{ \
	    cd $$CONFIG_TMP_DIR/manager ;\
	    kustomize edit set image controller=${CONTROLLER_IMG} ;\
	    kustomize edit add patch env_override.yaml ;\
	    kustomize edit add configmap controller-manager-env \
	        --from-literal=POSTGRES_IMAGE_NAME=${POSTGRES_IMAGE_NAME} ;\
	} ;\
	kustomize build $$CONFIG_TMP_DIR/default | kubectl apply -f - ;\
	rm -fr $$CONFIG_TMP_DIR

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

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

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifneq ($(shell controller-gen --version), Version: v0.2.5)
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.5 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif
