#!/usr/bin/env bash

##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2021 EnterpriseDB Corporation.
##

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

# Defaults
K8S_VERSION=${K8S_VERSION:-v1.21.1}
KUBECTL_VERSION=${KUBECTL_VERSION:-$K8S_VERSION}
CLUSTER_NAME=${CLUSTER_NAME:-pg-operator-e2e-${K8S_VERSION//./-}}
ENGINE="${CLUSTER_ENGINE:-kind}"
ENABLE_REGISTRY="${ENABLE_REGISTRY:-}"

# Define the directories used by the script
ROOT_DIR=$(realpath "$(dirname "$0")/../")
HACK_DIR="${ROOT_DIR}/hack"
E2E_DIR="${HACK_DIR}/e2e"
TEMP_DIR="$(mktemp -d)"
LOG_DIR=${LOG_DIR:-$ROOT_DIR/_logs/}
trap 'rm -fr ${TEMP_DIR}' EXIT

# Constants
registry_volume=registry_dev_data
registry_name=registry.dev

##
## KIND SUPPORT
##

install_kind() {
  local bindir=$1
  local binary="${bindir}/kind"
  local version

  # Get the latest release of kind unless specified in the environment
  version=${KIND_VERSION:-$(
    curl -s -LH "Accept:application/json" https://github.com/kubernetes-sigs/kind/releases/latest |
      sed 's/.*"tag_name":"\([^"]\+\)".*/\1/'
  )}

  curl -s -L "https://kind.sigs.k8s.io/dl/${version}/kind-$(uname)-amd64" -o "${binary}"
  chmod +x "${binary}"
}

load_image_kind() {
  local cluster_name=$1
  local image=$2
  kind load -v 1 docker-image --name "${cluster_name}" "${image}"
}

create_cluster_kind() {
  local k8s_version=$1
  local cluster_name=$2

  # Create kind config
  config_file="${TEMP_DIR}/kind-config.yaml"
  cp "${E2E_DIR}/kind-config.yaml" "${config_file}"
  if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ] || [ -n "${ENABLE_REGISTRY:-}" ]; then
    # Add containerdConfigPatches section
    cat >>"${config_file}" <<-EOF

containerdConfigPatches:
EOF

    if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
      cat >>"${config_file}" <<-EOF
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
    endpoint = ["${DOCKER_REGISTRY_MIRROR}"]
EOF
    fi

    if [ -n "${ENABLE_REGISTRY:-}" ]; then
      cat >>"${config_file}" <<-EOF
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."${registry_name}:5000"]
    endpoint = ["http://${registry_name}:5000"]
EOF
    fi
  fi
  # Create the cluster
  kind create cluster --name "${cluster_name}" --image "kindest/node:${k8s_version}" --config "${config_file}"

  if [ -n "${ENABLE_REGISTRY:-}" ]; then
    docker network connect "kind" "${registry_name}" &>/dev/null || true
  fi
}

export_logs_kind() {
  local cluster_name=$1
  kind export logs "${LOG_DIR}" --name "${cluster_name}"
}

destroy_kind() {
  local cluster_name=$1
  kind delete cluster --name "${cluster_name}" || true
}

##
## K3D SUPPORT
##

install_k3d() {
  local bindir=$1

  curl -s https://raw.githubusercontent.com/rancher/k3d/main/install.sh | K3D_INSTALL_DIR=$bindir bash -s -- --no-sudo
}

create_cluster_k3d() {
  local k8s_version=$1
  local cluster_name=$2

  local latest_k3s_tag
  latest_k3s_tag=$(curl -sL "https://registry.hub.docker.com/v1/repositories/rancher/k3s/tags" |
    jq -r '.[].name' | grep -- "^${k8s_version//./\\.}"'\+-k3s[0-9]$' | tail -n 1)

  local volumes=()
  if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ] || [ -n "${ENABLE_REGISTRY:-}" ]; then
    config_file="${TEMP_DIR}/k3d-registries.yaml"
    cat >"${config_file}" <<-EOF
mirrors:
EOF

    if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
      cat >>"${config_file}" <<-EOF
  "docker.io":
    endpoint:
      - "${DOCKER_REGISTRY_MIRROR}"
EOF
    fi

    if [ -n "${ENABLE_REGISTRY:-}" ]; then
      cat >>"${config_file}" <<-EOF
  "${registry_name}:5000":
    endpoint:
    - http://${registry_name}:5000
EOF
    fi

    volumes=(--volume "${config_file}:/etc/rancher/k3s/registries.yaml")
  fi

  k3d cluster create "${volumes[@]}" -a 3 -i "rancher/k3s:${latest_k3s_tag}" \
    --k3s-server-arg '--disable=traefik' --k3s-server-arg '--disable=metrics-server' --no-lb "${cluster_name}"

  if [ -n "${ENABLE_REGISTRY:-}" ]; then
    docker network connect "k3d-${cluster_name}" "${registry_name}" &>/dev/null || true
  fi
}

load_image_k3d() {
  local cluster_name=$1
  local image=$2
  k3d image import "${image}" -c "${cluster_name}"
}

export_logs_k3d() {
  local cluster_name=$1
  while IFS= read -r line; do
      NODES_LIST+=("$line")
  done < <(k3d node list | awk "/${cluster_name}/{print \$1}")
  for i in "${NODES_LIST[@]}"; do
      mkdir -p "${LOG_DIR}/${i}"
      docker cp -L "${i}:/var/log/." "${LOG_DIR}/${i}"
  done
}

destroy_k3d() {
  local cluster_name=$1
  k3d cluster delete "${cluster_name}" || true
}

##
## GENERIC ROUTINES
##

install_kubectl() {
  local bindir=$1

  local binary="${bindir}/kubectl"
  local os

  # Requires 'tr' for Darwin vs darwin issue
  os=$(uname | tr '[:upper:]' '[:lower:]')
  curl -sL "https://storage.googleapis.com/kubernetes-release/release/v${KUBECTL_VERSION#v}/bin/${os}/amd64/kubectl" -o "${binary}"
  chmod +x "${binary}"
}

# The following function makes sure we already have a Docker container
# with a bound volume to act as local registry. This is really needed
# to have an easy way to refresh the operator version that is running
# on the temporary cluster.
ensure_registry() {
  [ -z "${ENABLE_REGISTRY:-}" ] && return

  if ! docker volume inspect "${registry_volume}" &>/dev/null; then
    docker volume create "${registry_volume}"
  fi

  if ! docker inspect "${registry_name}" &>/dev/null; then
    docker container run -d --name "${registry_name}" -v "${registry_volume}:/var/lib/registry" --restart always -p 5000:5000 registry:2
  fi
}

deploy_fluentd() {
  local FLUENTD_IMAGE=fluent/fluentd-kubernetes-daemonset:v1-debian-forward
  local FLUENTD_LOCAL_IMAGE="${registry_name}:5000/fluentd-kubernetes-daemonset:local"

  docker pull "${FLUENTD_IMAGE}"
  docker tag "${FLUENTD_IMAGE}" "${FLUENTD_LOCAL_IMAGE}"
  load_image "${CLUSTER_NAME}" "${FLUENTD_LOCAL_IMAGE}"

  # Add fluentd service to export logs
  kubectl apply -f "${E2E_DIR}/local-fluentd.yaml"

  # Run the tests and destroy the cluster
  # Do not fail out if the tests fail. We want the logs anyway.
  ITER=0
  NODE=$(kubectl get nodes --no-headers | wc -l)
  while true; do
    if [[ $ITER -ge 300 ]]; then
      echo "Time out waiting for FluentD readiness"
      exit 1
    fi
    NUM_READY=$(kubectl get ds fluentd -n kube-system -o jsonpath='{.status.numberReady}')
    if [[ "$NUM_READY" == "$NODE" ]]; then
      echo "FluentD is Ready"
      break
    fi
    sleep 1
    ((++ITER))
  done
}

load_image_registry() {
  local image=$1

  local image_local_name=${image/${registry_name}/localhost}
  docker tag "${image}" "${image_local_name}"
  docker push "${image_local_name}"
}

load_image() {
  local cluster_name=$1
  local image=$2
  if [ -z "${ENABLE_REGISTRY:-}" ]; then
    "load_image_${ENGINE}" "${cluster_name}" "${image}"
  else
    load_image_registry "${image}"
  fi
}

deploy_operator() {
  kubectl delete ns postgresql-operator-system || :
  make -C "${ROOT_DIR}" deploy "CONTROLLER_IMG=${CONTROLLER_IMG}"
}


usage() {
  cat >&2 <<EOF
Usage: $0 [-e {kind|k3d}] [-k <version>] [-r] <command>

Commands:
    prepare <dest_dir>    Downloads the prerequisite into <dest_dir>
    create                Create the test cluster
    load                  Build and load the operator image in the cluster
    deploy                Deploy the operator manifests in the cluster
    print-image           Print the CONTROLLER_IMG name to be used inside
                          the cluster
    export-logs           Export the logs from the cluster inside the directory
                          ${LOG_DIR}
    destroy               Destroy the cluster

Options:
    -e|--engine
        <CLUSTE_ENGINE>   Use the provided ENGINE to run the cluster.
                          Available options are 'kind' and 'k3d'. Default 'kind'.
                          Env: CLUSTE_ENGINE

    -k|--k8s-version
        <K8S_VERSION>     Use the specified kubernetes full version number
                          (e.g., v1.21.1). Env: K8S_VERSION

    -r|--registry         Enable local registry. Env: ENABLE_REGISTRY
EOF
  exit 1
}

##
## COMMANDS
##

prepare() {
  local bindir=$1
  echo "Installing cluster prerequisites in ${bindir}"
  install_kubectl "${bindir}"
  "install_${ENGINE}" "${bindir}"
}

create() {
  "create_cluster_${ENGINE}" "${K8S_VERSION}" "${CLUSTER_NAME}"

  # Support for docker:dind service
  if [ "${DOCKER_HOST:-}" == "tcp://docker:2376" ]; then
    sed -i -E -e 's/0\.0\.0\.0/docker/g' "${HOME}/.kube/config"
  fi

  deploy_fluentd
}

load() {
  CONTROLLER_IMG="$(print_image)"
  make -C "${ROOT_DIR}" CONTROLLER_IMG="${CONTROLLER_IMG}" docker-build
  load_image "${CLUSTER_NAME}" "${CONTROLLER_IMG}"
}

deploy() {
  CONTROLLER_IMG="$(print_image)"
  deploy_operator
}

print_image() {
  echo "${registry_name}:5000/cloud-native-postgresql:latest"
}

export_logs() {
  "export_logs_${ENGINE}" "${CLUSTER_NAME}"
}

destroy() {
  "destroy_${ENGINE}" "${CLUSTER_NAME}"
}

##
## MAIN
##

main() {
  parsed_opts=$(getopt -o e:k:r -l "engine:,k8s-version:,registry" -- "$@") || usage
  eval "set -- $parsed_opts"
  for o; do
    case "${o}" in
    -e | --engine)
      shift
      ENGINE=$1
      shift
      if [ "${ENGINE}" != "kind" ] && [ "${ENGINE}" != "k3d" ]; then
        echo "ERROR: ${ENGINE} is not a valid engine! [kind, k3d]" >&2
        echo >&2
        usage
      fi
      ;;
    -k | --k8s-version)
      shift
      K8S_VERSION="v${1#v}"
      shift
      if ! [[ $K8S_VERSION =~ ^v1\.[0-9]+\.[0-9]+$ ]]; then
        echo "ERROR: $K8S_VERSION is not a valid k8s version!" >&2
        echo >&2
        usage
      fi
      ;;
    -r | --registry)
      shift
      ENABLE_REGISTRY=true
      ;;
    --)
      shift
      break
      ;;
    esac
  done

  # Check if command is missing
  if [ "$#" -eq 0 ]; then
    echo "ERROR: you must specify a command" >&2
    echo >&2
    usage
  fi
  command=$1
  shift

  # Invoke the command
  case "$command" in
  prepare)
    if [ "$#" -eq 0 ]; then
      echo "ERROR: prepare requires a destination directory" >&2
      echo >&2
      usage
    fi
    prepare "$1"
    ;;

  create | load | deploy | print-image | export-logs | destroy)
    ensure_registry
    "${command//-/_}"
    ;;
  *)
    echo "ERROR: unknown command ${command}" >&2
    echo >&2
    usage
    ;;
  esac
}

main "$@"
