# This file is part of Cloud Native PostgreSQL.
#
# Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.

# Image URL to use all building/pushing image targets
CONTROLLER_IMG ?= internal.2ndq.io/k8s/cloud-native-postgresql:latest
BUILD_IMAGE ?= true
POSTGRES_IMAGE_NAME ?= quay.io/2ndquadrant/postgres:latest

# RedHat Operator Hub references / indexes
OPERATOR_HUB_VERSION=0.0.65
BUNDLE_IMAGE=internal.2ndq.io/k8s/cloud-native-postgresql:${OPERATOR_HUB_VERSION}-bundle
INDEX_IMAGE=internal.2ndq.io/k8s/cloud-native-postgresql:${OPERATOR_HUB_VERSION}-index

export CONTROLLER_IMG BUILD_IMAGE POSTGRES_IMAGE_NAME BUNDLE_IMAGE INDEX_IMAGE

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
	    cd $$CONFIG_TMP_DIR/default ;\
	    kustomize edit add patch manager_image_pull_secret.yaml ;\
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
	GOPRIVATE="gitlab.2ndquadrant.com/*" $(GO_LICENSES) \
		save gitlab.2ndquadrant.com/k8s/cloud-native-postgresql \
		--save_path licenses/go-licenses --force
	chmod a+rw -R licenses/go-licenses

# OLM bundle
olm-bundle: manifests opm
	set -xe ;\
	CONFIG_TMP_DIR=$$(mktemp -d) ;\
	cp -r config/* $$CONFIG_TMP_DIR ;\
	( \
	    cd $$CONFIG_TMP_DIR/manager ;\
	    kustomize edit set image controller=${CONTROLLER_IMG} ;\
	    kustomize edit add patch openshift_override.yaml ;\
	    cd $$CONFIG_TMP_DIR/rbac ;\
	    cat kustomization.openshift.yaml >> kustomization.yaml ;\
	) ;\
	(kustomize build $$CONFIG_TMP_DIR/default; echo '---'; kustomize build $$CONFIG_TMP_DIR/samples) | \
	operator-sdk generate bundle --verbose --overwrite --channels alpha,beta,stable --default-channel beta --version ${OPERATOR_HUB_VERSION} ;\
	rm -fr $$CONFIG_TMP_DIR ;\
	docker build --no-cache -f bundle.Dockerfile -t ${BUNDLE_IMAGE} . ;\
	$(OPM) index add --bundles ${BUNDLE_IMAGE} --tag ${INDEX_IMAGE} --build-tool docker --pull-tool none

olm-bundle-push: olm-bundle
	docker push ${BUNDLE_IMAGE}
	docker push ${INDEX_IMAGE}

# find or download opm
.PHONY: opm
opm:
# download opm if necessary
ifeq (, $(shell which opm))
	@{ \
	set -e ;\
	OPM_VERSION=$$(curl -s -LH "Accept:application/json" https://github.com/operator-framework/operator-registry/releases/latest | sed 's/.*"tag_name":"\([^"]\+\)".*/\1/') ;\
	curl -s -L "https://github.com/operator-framework/operator-registry/releases/download/$${OPM_VERSION}/$$(uname | tr '[:upper:]' '[:lower:]')-amd64-opm" -o "$(GOBIN)/opm" ;\
	chmod +x "$(GOBIN)/opm" ;\
	}
OPM=$(GOBIN)/opm
else
OPM=$(shell which opm)
endif

# find or download controller-gen
.PHONY: controller-gen
controller-gen:
# download controller-gen if necessary
ifneq ($(shell controller-gen --version), Version: v0.3.0)
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.3.0 ;\
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
