#!/usr/bin/env bash

##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
##

# standard bash error handling
set -eEuo pipefail;

PRESERVE_CLUSTER=${PRESERVE_CLUSTER:-false}
DEBUG=${DEBUG:-false}
K8S_VERSION=${K8S_VERSION:-1.17.2}

# Define the directories used by the tests
ROOT_DIR=$(realpath "$(dirname "$0")/../../")
TEST_DIR="${ROOT_DIR}/tests/e2e"
HACK_DIR="${ROOT_DIR}/hack/e2e"
TEMP_DIR="$(mktemp -d)"

# Get the latest releases of kind and kubectl unless specified in the environment
KIND="${TEMP_DIR}/kind"
KUBECTL="${TEMP_DIR}/kubectl"
KIND_VERSION=${KIND_VERSION:-$(curl --silent https://api.github.com/repos/kubernetes-sigs/kind/releases/latest | grep '"tag_name"' | cut -d \" -f 4)}
KUBECTL_VERSION=${KUBECTL_VERSION:-$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)}

KIND_CLUSTER_NAME=pg-operator-e2e-${K8S_VERSION}

if [ "${DEBUG}" = true ]; then
    set -x
fi

cleanup() {
    if [ "${PRESERVE_CLUSTER}" = false ]; then
        "${KIND}" delete cluster --name "${KIND_CLUSTER_NAME}" || true
        rm -rf "${TEMP_DIR}"
    else
        echo "You've chosen not to delete the Kubernetes cluster."
        echo "You can delete it manually later running:"
        echo "${KIND} delete cluster --name '${KIND_CLUSTER_NAME}'"
        echo "rm -rf ${TEMP_DIR}"
    fi
}
trap cleanup EXIT

install_kubectl() {
    # Requires 'tr' for Darwin vs darwin issue
    curl -s -L "https://storage.googleapis.com/kubernetes-release/release/${KUBECTL_VERSION}/bin/$(uname | tr '[:upper:]' '[:lower:]')/amd64/kubectl" -o "${KUBECTL}"
    chmod +x "${KUBECTL}"
}

install_kind() {
    curl -s -L "https://github.com/kubernetes-sigs/kind/releases/download/${KIND_VERSION}/kind-$(uname)-amd64" -o "${KIND}"
    chmod +x "${KIND}"
}

main() {
    # Add kubectl, kind and ginkgo to the path
    export PATH="${TEMP_DIR}:$(go env GOPATH)/bin:${PATH}"

    install_kubectl
    install_kind

    # Set kind verbosity
    if [ "${DEBUG}" = true ]; then
        verbosity='-v 1'
    else
        verbosity='-q'
    fi

    "${KIND}" create cluster ${verbosity} \
        --config "${HACK_DIR}/kind-config.yaml" \
        --name "${KIND_CLUSTER_NAME}" --image=kindest/node:v${K8S_VERSION}

    # Support for docker:dind service
    if [ "${DOCKER_HOST:-}" == "tcp://docker:2376" ]
    then
        sed -i -E -e 's/0\.0\.0\.0/docker/g' "${HOME}/.kube/config"
    fi

    "${HACK_DIR}/kind-deploy-operator.sh" "${KIND_CLUSTER_NAME}"

    # Install ginkgo cli for better control on tests
    go install github.com/onsi/ginkgo/ginkgo
    # Create at most 4 testing nodes. Using -p instead of --nodes
    # would create CPUs-1 nodes and saturate the testing server
    ginkgo --nodes=4 --slowSpecThreshold=30 -v "${TEST_DIR}/..."
}

main