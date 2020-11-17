#!/usr/bin/env bash

##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2020 2ndQuadrant Limited
##

# standard bash error handling
set -eEuo pipefail;

if [ "${DEBUG-}" = true ]; then
    set -x
fi

# Define the directories used by the tests
ROOT_DIR=$(realpath "$(dirname "$0")/../../")
HACK_DIR="${ROOT_DIR}/hack/e2e"
TEMP_DIR=$(mktemp -d)
LOG_DIR=${LOG_DIR:-$ROOT_DIR/kind-logs/}

export CONTROLLER_IMG=${CONTROLLER_IMG:-internal.2ndq.io/k8s/cloud-native-postgresql:latest}
export POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG}.0}

PRESERVE_CLUSTER=${PRESERVE_CLUSTER:-false}
BUILD_IMAGE=${BUILD_IMAGE:-false}
K8S_VERSION=${K8S_VERSION:-v1.19.1}
KUBECTL_VERSION=${KUBECTL_VERSION:-$K8S_VERSION}

# Get the latest release of kind unless specified in the environment
KIND="${TEMP_DIR}/kind"
KIND_VERSION=${KIND_VERSION:-$(curl -s -LH "Accept:application/json" https://github.com/kubernetes-sigs/kind/releases/latest | sed 's/.*"tag_name":"\([^"]\+\)".*/\1/')}
KIND_CLUSTER_NAME=pg-operator-e2e-${K8S_VERSION}

KUBECTL="${TEMP_DIR}/kubectl"

cleanup() {
    if [ "${PRESERVE_CLUSTER}" = false ]; then
        kubetest2 kind --down --cluster-name "${KIND_CLUSTER_NAME}" || true
        rm -rf "${TEMP_DIR}"
    else
        set +x
        echo "You've chosen not to delete the Kubernetes cluster."
        echo "You can delete it manually later running:"
        echo "kubetest2 kind --down --cluster-name ${KIND_CLUSTER_NAME}"
        echo "rm -rf ${TEMP_DIR}"
    fi
}
trap cleanup EXIT

install_kubectl() {
    # We can't test a using kubectl version 1.15.x
    # need to raise the version to 1.16.x
    # see https://github.com/kubernetes/kubernetes/issues/80515
    if [[ $K8S_VERSION =~ ^v?1.15 ]]; then
        KUBECTL_VERSION=v1.16.9
    fi
    # Requires 'tr' for Darwin vs darwin issue
    curl -sL "https://storage.googleapis.com/kubernetes-release/release/v${KUBECTL_VERSION#v}/bin/$(uname | tr '[:upper:]' '[:lower:]')/amd64/kubectl" -o "${KUBECTL}"
    chmod +x "${KUBECTL}"
}

install_kind() {
    curl -s -L "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-$(uname)-amd64" -o "${KIND}"
    chmod +x "${KIND}"
}

install_go_tools() {
    local GO_TMP_DIR
    GO_TMP_DIR=$(mktemp -d)
    cd "$GO_TMP_DIR"
    go mod init tmp
    go get sigs.k8s.io/kubetest2/...@latest
    go get -u github.com/onsi/ginkgo/ginkgo
    rm -rf "$GO_TMP_DIR"
    cd -
}

build_and_load_operator() {
    docker build -t "${CONTROLLER_IMG}" "${ROOT_DIR}"
    kind load -v 1 docker-image --name "${KIND_CLUSTER_NAME}" "${CONTROLLER_IMG}"
}

main() {
    # Add kubectl, kind, kubetest and ginkgo to the path
    PATH="${TEMP_DIR}:$(go env GOPATH)/bin:${PATH}"
    export PATH
    install_kubectl
    install_kind
    install_go_tools

    # Create kind config
    config_file=$(mktemp)
    cp "${HACK_DIR}/kind-config.yaml" "${config_file}"
    if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
	cat >> "${config_file}" <<-EOF

	containerdConfigPatches:
	- |-
	  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."${DOCKER_REGISTRY_MIRROR##*//}"]
	    endpoint = ["${DOCKER_REGISTRY_MIRROR}"]

	EOF
    fi

    # Create the cluster
    kubetest2 kind --up --image-name "kindest/node:${K8S_VERSION}" \
        --cluster-name "${KIND_CLUSTER_NAME}" \
        --config "${config_file}"

    # Support for docker:dind service
    if [ "${DOCKER_HOST:-}" == "tcp://docker:2376" ]; then
        sed -i -E -e 's/0\.0\.0\.0/docker/g' "${HOME}/.kube/config"
    fi

    # Build an image or use a pull secret
    if [ "${BUILD_IMAGE}" = false ]; then
        export DOCKER_SERVER
        export DOCKER_USERNAME
        export DOCKER_PASSWORD
    else
        export CONTROLLER_IMG=cloud-native-postgresql:e2e
        build_and_load_operator
    fi

    ${KUBECTL} apply -f "${HACK_DIR}/kind-fluentd.yaml"
    # Run the tests and destroy the cluster
    # Do not fail out if the tests fail. We want the logs anyway.
    RC=0
    kubetest2 kind --test exec --cluster-name "${KIND_CLUSTER_NAME}" \
        -- "${HACK_DIR}/run-e2e.sh" || RC=$?
    kind export logs "${LOG_DIR}" --name "${KIND_CLUSTER_NAME}"
    if [ "${PRESERVE_CLUSTER}" = false ]; then
        kubetest2 kind --down --cluster-name "${KIND_CLUSTER_NAME}"
    fi
    exit $RC
}

main
