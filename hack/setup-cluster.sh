#!/usr/bin/env bash

##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2021 EnterpriseDB Corporation.
##

# standard bash error handling
set -eEuo pipefail;

if [ "${DEBUG-}" = true ]; then
    set -x
fi


# Kubernetes cluster will be created with kind by default
# Possible values: 'kind' or 'k3d'
# i.e. CLUSTER_ENGINE=k3d ./setup-cluster.sh
ENGINE="${CLUSTER_ENGINE:-kind}"

if [ "${ENGINE}" != "kind" ] && [ "${ENGINE}" != "k3d" ]; then
    echo "ERROR: not a valid Cluster Engine!"
    echo ""
    echo "Usage: "
    echo "    CLUSTER_ENGINE=k3d $0"
    echo ""
    echo "Hint: Valid values are 'kind' (default) or 'k3d'."
    exit 1
fi

# Define the directories used by the tests
ROOT_DIR=$(realpath "$(dirname "$0")/../")
HACK_DIR="${ROOT_DIR}/hack"
E2E_DIR="${HACK_DIR}/e2e"
TEMP_DIR=${TEMP_DIR:-$(mktemp -d)}

PRESERVE_CLUSTER=${PRESERVE_CLUSTER:-true}
BUILD_IMAGE=${BUILD_IMAGE:-false}
K8S_VERSION=${K8S_VERSION:-v1.20.0}
KUBECTL_VERSION=${KUBECTL_VERSION:-$K8S_VERSION}
CLUSTER_NAME=${CLUSTER_NAME:-pg-operator-e2e-${K8S_VERSION//./-}}

KUBECTL="${TEMP_DIR}/kubectl"

cleanup_k3d() {
    if [ "${PRESERVE_CLUSTER}" = false ]; then
      k3d cluster delete "${CLUSTER_NAME}" || true
      rm -rf "${TEMP_DIR}"
    fi
}

cleanup_kind() {
    if [ "${PRESERVE_CLUSTER}" = false ]; then
      kind delete cluster --name "${CLUSTER_NAME}" || true
      rm -rf "${TEMP_DIR}"
    fi
}

trap cleanup_${ENGINE} ERR

manual_cleanup_k3d() {
    set +x
    echo "You can delete it manually later running:"
    echo "k3d cluster delete ${CLUSTER_NAME}"
    echo "rm -rf ${TEMP_DIR}"
}

manual_cleanup_kind() {
    set +x
    echo "You can delete it manually later running:"
    echo "kind delete cluster --name ${CLUSTER_NAME}"
    echo "rm -rf ${TEMP_DIR}"
}

install_kubectl() {
    # Requires 'tr' for Darwin vs darwin issue
    curl -sL "https://storage.googleapis.com/kubernetes-release/release/v${KUBECTL_VERSION#v}/bin/$(uname | tr '[:upper:]' '[:lower:]')/amd64/kubectl" -o "${KUBECTL}"
    chmod +x "${KUBECTL}"
}

install_k3d() {
    curl -s https://raw.githubusercontent.com/rancher/k3d/main/install.sh -o "${TEMP_DIR}/install.sh"
    chmod +x "${TEMP_DIR}/install.sh"
    K3D_INSTALL_DIR=${TEMP_DIR} "${TEMP_DIR}"/install.sh --no-sudo
}

install_kind() {
    # Get the latest release of kind unless specified in the environment
    KIND="${TEMP_DIR}/kind"
    KIND_VERSION=${KIND_VERSION:-$(curl -s -LH "Accept:application/json" https://github.com/kubernetes-sigs/kind/releases/latest | sed 's/.*"tag_name":"\([^"]\+\)".*/\1/')}

    curl -s -L "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-$(uname)-amd64" -o "${KIND}"
    chmod +x "${KIND}"
}

build_and_load_operator_k3d() {
    docker build -t "${CONTROLLER_IMG}" "${ROOT_DIR}"
    k3d image import "${CONTROLLER_IMG}" -c "${CLUSTER_NAME}"
}

build_and_load_operator_kind() {
    docker build -t "${CONTROLLER_IMG}" "${ROOT_DIR}"
    kind load -v 1 docker-image --name "${CLUSTER_NAME}" "${CONTROLLER_IMG}"
}

create_cluster_k3d() {
    # Create the cluster
    # TODO: Evaluate a better way to define
    # the latest K3S version released
    K3S_VERSION=5
    while ! docker pull -q rancher/k3s:${K8S_VERSION}-k3s${K3S_VERSION}
    do
      let K3S_VERSION--
    done

    if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
      config_file=$(mktemp)
cat > "${config_file}" <<-EOF
mirrors:
  "docker.io":
    endpoint:
      - "${DOCKER_REGISTRY_MIRROR}"
EOF
      k3d cluster create --volume "${config_file}:/etc/rancher/k3s/registries.yaml" -a 3 -i rancher/k3s:${K8S_VERSION}-k3s${K3S_VERSION} "${CLUSTER_NAME}"
    else
      k3d cluster create -a 3 -i rancher/k3s:${K8S_VERSION}-k3s${K3S_VERSION} "${CLUSTER_NAME}"
    fi
}

create_cluster_kind() {
    # Create kind config
    config_file=$(mktemp)
    cp "${E2E_DIR}/kind-config.yaml" "${config_file}"
    if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
cat >> "${config_file}" <<-EOF

containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["${DOCKER_REGISTRY_MIRROR}"]

EOF
    fi
    # Create the cluster
    kind create cluster --name "${CLUSTER_NAME}" --image "kindest/node:${K8S_VERSION}" --config "${config_file}"
}

main() {
    # Add kubectl, $ENGINE to the path
    PATH="${TEMP_DIR}:${PATH}"
    export PATH

    install_kubectl
    install_${ENGINE}
    create_cluster_${ENGINE}

    # Support for docker:dind service
    if [ "${DOCKER_HOST:-}" == "tcp://docker:2376" ]; then
        sed -i -E -e 's/0\.0\.0\.0/docker/g' "${HOME}/.kube/config"
    fi

    # Build an image or use a pull secret
    if [ "${BUILD_IMAGE}" = true ]; then
        CONTROLLER_IMG=${CONTROLLER_IMG:-cloud-native-postgresql:local}
        build_and_load_operator_${ENGINE}
    fi

    if [ "${PRESERVE_CLUSTER}" = true ]; then
        manual_cleanup_${ENGINE}
    fi
}


main
