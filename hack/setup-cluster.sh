#!/usr/bin/env bash

##
## Copyright © contributors to CloudNativePG, established as
## CloudNativePG a Series of LF Projects, LLC.
##
## Licensed under the Apache License, Version 2.0 (the "License");
## you may not use this file except in compliance with the License.
## You may obtain a copy of the License at
##
##     http://www.apache.org/licenses/LICENSE-2.0
##
## Unless required by applicable law or agreed to in writing, software
## distributed under the License is distributed on an "AS IS" BASIS,
## WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
## See the License for the specific language governing permissions and
## limitations under the License.
##
## SPDX-License-Identifier: Apache-2.0
##

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

# Defaults
KIND_NODE_DEFAULT_VERSION=v1.33.2
CSI_DRIVER_HOST_PATH_DEFAULT_VERSION=v1.17.0
EXTERNAL_SNAPSHOTTER_VERSION=v8.3.0
EXTERNAL_PROVISIONER_VERSION=v5.3.0
EXTERNAL_RESIZER_VERSION=v1.14.0
EXTERNAL_ATTACHER_VERSION=v4.9.0
K8S_VERSION=${K8S_VERSION-}
KUBECTL_VERSION=${KUBECTL_VERSION-}
CSI_DRIVER_HOST_PATH_VERSION=${CSI_DRIVER_HOST_PATH_VERSION:-$CSI_DRIVER_HOST_PATH_DEFAULT_VERSION}
ENABLE_PYROSCOPE=${ENABLE_PYROSCOPE:-}
ENABLE_CSI_DRIVER=${ENABLE_CSI_DRIVER:-}
ENABLE_APISERVER_AUDIT=${ENABLE_APISERVER_AUDIT:-}
NODES=${NODES:-3}
# This option is telling the docker to use node image with certain arch, i.e kindest/node in kind.
# In M1/M2,  if enable amd64 emulation then we keep it as linux/amd64.
# if did not enable amd64 emulation we need keep it as linux/arm64,  otherwise,  kind will not start success
DOCKER_DEFAULT_PLATFORM=${DOCKER_DEFAULT_PLATFORM:-}
# Testing the upgrade will require generating a second operator image, `-prime`
# The `load()` function will build and push this second image by default.
# The TEST_UPGRADE_TO_V1 can be set to false to skip this part of `load()`
TEST_UPGRADE_TO_V1=${TEST_UPGRADE_TO_V1:-true}

# Define the directories used by the script
ROOT_DIR=$(cd "$(dirname "$0")/../"; pwd)
HACK_DIR="${ROOT_DIR}/hack"
E2E_DIR="${HACK_DIR}/e2e"
TEMP_DIR="$(mktemp -d)"
LOG_DIR=${LOG_DIR:-$ROOT_DIR/_logs/}
trap 'rm -fr ${TEMP_DIR}' EXIT

# Architecture
ARCH=$(uname -m)
case $ARCH in
  x86_64) ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac

# If user did not set it explicitly
if [ "${DOCKER_DEFAULT_PLATFORM}" = "" ]; then
  DOCKER_DEFAULT_PLATFORM="linux/${ARCH}"
fi
export DOCKER_DEFAULT_PLATFORM

# Constants
registry_volume=registry_dev_data
registry_name=registry.dev
registry_net=registry
builder_name=cnpg-builder

# #########################################################################
# IMPORTANT: here we build a catalog of images that will be needed in the
# test run. The goal here is to pre-load all the images that are part of the
# HELPER_IMGS variable in the local container registry.
# #########################################################################
POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}
PGBOUNCER_IMG=${PGBOUNCER_IMG:-$(grep 'DefaultPgbouncerImage.*=' "${ROOT_DIR}/pkg/specs/pgbouncer/deployments.go" | cut -f 2 -d \")}
MINIO_IMG=${MINIO_IMG:-$(grep 'minioImage.*=' "${ROOT_DIR}/tests/utils/minio/minio.go"  | cut -f 2 -d \")}
APACHE_IMG=${APACHE_IMG:-"httpd"}

HELPER_IMGS=("$POSTGRES_IMG" "$E2E_PRE_ROLLING_UPDATE_IMG" "$PGBOUNCER_IMG" "$MINIO_IMG" "$APACHE_IMG")
# #########################################################################

# Colors (only if using a terminal)
bright=
reset=
if [ -t 1 ]; then
  bright=$(tput bold 2>/dev/null || true)
  reset=$(tput sgr0 2>/dev/null || true)
fi

##
## KIND SUPPORT
##

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
  cat >"${config_file}" <<-EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  apiServerAddress: "0.0.0.0"

# add to the apiServer certSANs the name of the docker (dind) service in order to be able to reach the cluster through it
kubeadmConfigPatchesJSON6902:
  - group: kubeadm.k8s.io
    version: v1beta2
    kind: ClusterConfiguration
    patch: |
      - op: add
        path: /apiServer/certSANs/-
        value: docker
nodes:
- role: control-plane
EOF
  if [ "${ENABLE_APISERVER_AUDIT}" = "true" ]; then
    # Create the apiserver audit log directory beforehand, otherwise it will be
    # generated within docker with root permissions
    mkdir -p "${LOG_DIR}/apiserver"
    touch "${LOG_DIR}/apiserver/kube-apiserver-audit.log"
    cat >>"${config_file}" <<-EOF
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
        # enable auditing flags on the API server
        extraArgs:
          audit-log-path: /var/log/kubernetes/kube-apiserver-audit.log
          audit-policy-file: /etc/kubernetes/policies/audit-policy.yaml
        # mount new files / directories on the control plane
        extraVolumes:
          - name: audit-policies
            hostPath: /etc/kubernetes/policies
            mountPath: /etc/kubernetes/policies
            readOnly: true
            pathType: "DirectoryOrCreate"
          - name: "audit-logs"
            hostPath: "/var/log/kubernetes"
            mountPath: "/var/log/kubernetes"
            readOnly: false
            pathType: DirectoryOrCreate
  # mount the local file on the control plane
  extraMounts:
  - hostPath: ${E2E_DIR}/audit-policy.yaml
    containerPath: /etc/kubernetes/policies/audit-policy.yaml
    readOnly: true
  - hostPath: ${LOG_DIR}/apiserver/
    containerPath: /var/log/kubernetes/
EOF
  fi

  if [ "$NODES" -gt 1 ]; then
    for ((i = 0; i < NODES; i++)); do
      echo '- role: worker' >>"${config_file}"
    done
  fi

  # Enable ImageVolume support from kindest/node v1.33.1
  if [[ "$(printf '%s\n' "1.33.1" "${k8s_version#v}" | sort -V | head -n1)" == "1.33.1" ]]; then
    cat >>"${config_file}" <<-EOF

featureGates:
  ImageVolume: true
EOF
  fi

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

  cat >>"${config_file}" <<-EOF
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."${registry_name}:5000"]
    endpoint = ["http://${registry_name}:5000"]
EOF

  # Create the cluster
  kind create cluster --name "${cluster_name}" --image "kindest/node:${k8s_version}" --config "${config_file}"

  docker network connect "kind" "${registry_name}" &>/dev/null || true

  # Workaround for https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files
  for node in $(kind get nodes --name "${cluster_name}"); do
    docker exec "$node" sysctl fs.inotify.max_user_watches=524288 fs.inotify.max_user_instances=512
  done
}

export_logs_kind() {
  local cluster_name=$1
  kind export logs "${LOG_DIR}" --name "${cluster_name}"
}

destroy_kind() {
  local cluster_name=$1
  docker network disconnect "kind" "${registry_name}" &>/dev/null || true
  kind delete cluster --name "${cluster_name}" || true
  docker network rm "kind" &>/dev/null || true
}

##
## GENERIC ROUTINES
##

# The following function makes sure we already have a Docker container
# with a bound volume to act as local registry. This is really needed
# to have an easy way to refresh the operator version that is running
# on the temporary cluster.
ensure_registry() {
  if ! docker volume inspect "${registry_volume}" &>/dev/null; then
    docker volume create "${registry_volume}"
  fi

  if ! docker network inspect "${registry_net}" &>/dev/null; then
    docker network create "${registry_net}"
  fi

  if ! docker inspect "${registry_name}" &>/dev/null; then
    docker container run -d --name "${registry_name}" --network "${registry_net}" -v "${registry_volume}:/var/lib/registry" --restart always -p 5000:5000 registry:2
  fi
}

# An existing builder will not have any knowledge of the local registry or the
# any host outside the builder, but when having the builder inside Kubernetes
# this is fixed since we already solved the issue of the kubernetes cluster reaching
# out the local registry. The following functions will handle that builder
create_builder() {
  docker buildx rm "${builder_name}" &>/dev/null || true
  docker buildx create --name "${builder_name}" --driver-opt "network=${registry_net}"
}

deploy_fluentd() {
  local FLUENTD_IMAGE=fluent/fluentd-kubernetes-daemonset:v1.14.3-debian-forward-1.0
  local FLUENTD_LOCAL_IMAGE="${registry_name}:5000/fluentd-kubernetes-daemonset:local"

  docker pull "${FLUENTD_IMAGE}"
  docker tag "${FLUENTD_IMAGE}" "${FLUENTD_LOCAL_IMAGE}"
  load_image "${CLUSTER_NAME}" "${FLUENTD_LOCAL_IMAGE}"

  # Add fluentd service to export logs
  kubectl apply -f "${E2E_DIR}/local-fluentd.yaml"

  # Run the tests and destroy the cluster
  # Do not fail out if the tests fail. We want the logs anyway.
  ITER=0
  NODE=$(kubectl get nodes --no-headers | wc -l | tr -d " ")
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

deploy_csi_host_path() {
  echo "${bright}Starting deployment of CSI driver plugin... ${reset}"
  CSI_BASE_URL=https://raw.githubusercontent.com/kubernetes-csi

  ## Install external snapshotter CRD
  kubectl apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml
  kubectl apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml
  kubectl apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml
  kubectl apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml
  kubectl apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml
  kubectl apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/deploy/kubernetes/csi-snapshotter/rbac-csi-snapshotter.yaml

  ## Install external provisioner
  kubectl apply -f "${CSI_BASE_URL}"/external-provisioner/"${EXTERNAL_PROVISIONER_VERSION}"/deploy/kubernetes/rbac.yaml

  ## Install external attacher
  kubectl apply -f "${CSI_BASE_URL}"/external-attacher/"${EXTERNAL_ATTACHER_VERSION}"/deploy/kubernetes/rbac.yaml

  ## Install external resizer
  kubectl apply -f "${CSI_BASE_URL}"/external-resizer/"${EXTERNAL_RESIZER_VERSION}"/deploy/kubernetes/rbac.yaml

  ## Install driver and plugin
  ## Create a temporary file for the modified plugin deployment. This is needed
  ## because csi-driver-host-path plugin yaml tends to lag behind a few versions.
  plugin_file="${TEMP_DIR}/csi-hostpath-plugin.yaml"
  curl -sSL "${CSI_BASE_URL}/csi-driver-host-path/${CSI_DRIVER_HOST_PATH_VERSION}/deploy/kubernetes-1.30/hostpath/csi-hostpath-plugin.yaml" |
    sed "s|registry.k8s.io/sig-storage/hostpathplugin:.*|registry.k8s.io/sig-storage/hostpathplugin:${CSI_DRIVER_HOST_PATH_VERSION}|g" > "${plugin_file}"

  kubectl apply -f "${CSI_BASE_URL}"/csi-driver-host-path/"${CSI_DRIVER_HOST_PATH_VERSION}"/deploy/kubernetes-1.30/hostpath/csi-hostpath-driverinfo.yaml
  kubectl apply -f "${plugin_file}"
  rm "${plugin_file}"

  ## create volumesnapshotclass
  kubectl apply -f "${CSI_BASE_URL}"/csi-driver-host-path/"${CSI_DRIVER_HOST_PATH_VERSION}"/deploy/kubernetes-1.30/hostpath/csi-hostpath-snapshotclass.yaml

  ## Prevent VolumeSnapshot E2e test to fail when taking a
  ## snapshot of a running PostgreSQL instance
  kubectl patch volumesnapshotclass csi-hostpath-snapclass -p '{"parameters":{"ignoreFailedRead":"true"}}' --type merge

  ## create storage class
  kubectl apply -f "${CSI_BASE_URL}"/csi-driver-host-path/"${CSI_DRIVER_HOST_PATH_VERSION}"/examples/csi-storageclass.yaml
  kubectl annotate storageclass csi-hostpath-sc storage.kubernetes.io/default-snapshot-class=csi-hostpath-snapclass

  echo "${bright} CSI driver plugin deployment has started. Waiting for the CSI plugin to be ready... ${reset}"
  ITER=0
  while true; do
    if [[ $ITER -ge 300 ]]; then
      echo "${bright}Timeout: The CSI plugin did not become ready within the expected time.${reset}"
      exit 1
    fi
    NUM_SPEC=$(kubectl get statefulset csi-hostpathplugin  -o jsonpath='{.spec.replicas}')
    NUM_STATUS=$(kubectl get statefulset csi-hostpathplugin -o jsonpath='{.status.availableReplicas}')
    if [[ "$NUM_SPEC" == "$NUM_STATUS" ]]; then
      echo "${bright}Success: The CSI plugin is deployed and ready.${reset}"
      break
    fi
    sleep 1
    ((++ITER))
  done
}


deploy_pyroscope() {
  helm repo add pyroscope-io https://grafana.github.io/helm-charts

  values_file="${TEMP_DIR}/pyroscope_values.yaml"
  cat >"${values_file}" <<-EOF
pyroscopeConfigs:
  log-level: "debug"
EOF
  helm -n cnpg-system install pyroscope pyroscope-io/pyroscope -f "${values_file}"

  service_file="${TEMP_DIR}/pyroscope_service.yaml"

  cat >"${service_file}" <<-EOF
apiVersion: v1
kind: Service
metadata:
  name: cnpg-pprof
spec:
  ports:
  - targetPort: 6060
    port: 6060
  selector:
    app: cnpg-pprof
  type: ClusterIP
  selector:
    app.kubernetes.io/name: cloudnative-pg
EOF
  kubectl -n cnpg-system apply -f "${service_file}"

  annotations="${TEMP_DIR}/pyroscope_annotations.yaml"
  cat >"${annotations}" <<- EOF
spec:
   template:
      metadata:
         annotations:
            profiles.grafana.com/memory.scrape: "true"
            profiles.grafana.com/memory.port: "6060"
            profiles.grafana.com/cpu.scrape: "true"
            profiles.grafana.com/cpu.port: "6060"
            profiles.grafana.com/goroutine.scrape: "true"
            profiles.grafana.com/goroutine.port: "6060"
EOF

  kubectl -n cnpg-system patch deployment cnpg-controller-manager --patch-file "${annotations}"
}

deploy_prometheus_crds() {
  echo "${bright}Starting deployment of Prometheus CRDs... ${reset}"
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
  helm -n kube-system install prometheus-operator-crds prometheus-community/prometheus-operator-crds
}

load_image_registry() {
  local image=$1

  local image_local_name=${image/${registry_name}/127.0.0.1}
  docker tag "${image}" "${image_local_name}"
  docker push --platform "${DOCKER_DEFAULT_PLATFORM}" -q "${image_local_name}"
}

load_image() {
  local cluster_name=$1
  local image=$2
  load_image_registry "${image}"
}

deploy_operator() {
  kubectl delete ns cnpg-system 2> /dev/null || :

  make -C "${ROOT_DIR}" deploy "CONTROLLER_IMG=${CONTROLLER_IMG}"
}

usage() {
  cat >&2 <<EOF
Usage: $0 [-k <version>] [-r] <command>

Commands:
    create                Create the test cluster and a local registry
    load                  Build and load the operator image in the cluster
    load-helper-images    Load the catalog of HELPER_IMGS into the local registry
    deploy                Deploy the operator manifests in the cluster
    print-image           Print the CONTROLLER_IMG name to be used inside
                          the cluster
    export-logs           Export the logs from the cluster inside the directory
                          ${LOG_DIR}
    destroy               Destroy the cluster
    pyroscope             Deploy Pyroscope inside operator namespace

Options:
    -k|--k8s-version
        <K8S_VERSION>     Use the specified kubernetes full version number
                          (e.g., v1.27.0). Env: K8S_VERSION

    -n|--nodes
        <NODES>           Create a cluster with the required number of nodes.
                          Used only during "create" command. Default: 3
                          Env: NODES

To use long options you need to have GNU enhanced getopt available, otherwise
you can only use the short version of the options.
EOF
  exit 1
}

##
## COMMANDS
##

create() {
  echo "${bright}Creating kind cluster ${CLUSTER_NAME} with version ${K8S_VERSION}${reset}"

  "create_cluster_kind" "${K8S_VERSION}" "${CLUSTER_NAME}"

  # Support for docker:dind service
  if [ "${DOCKER_HOST:-}" == "tcp://docker:2376" ]; then
    sed -i -E -e 's/0\.0\.0\.0/docker/g' "${HOME}/.kube/config"
  fi

  deploy_fluentd
  deploy_csi_host_path
  deploy_prometheus_crds

  echo "${bright}Done creating kind cluster ${CLUSTER_NAME} with version ${K8S_VERSION}${reset}"
}

load_helper_images() {
  echo "${bright}Loading helper images for tests on cluster ${CLUSTER_NAME}${reset}"

  # Here we pre-load all the images defined in the HELPER_IMGS variable
  # with the goal to speed up the runs.
  for IMG in "${HELPER_IMGS[@]}"; do
    docker pull "${IMG}"
    "load_image_kind" "${CLUSTER_NAME}" "${IMG}"
  done

  echo "${bright}Done loading helper images on cluster ${CLUSTER_NAME}${reset}"
}

load() {
  # NOTE: this function will build the operator from the current source
  # tree and push it either to the local registry or the cluster nodes.
  # It will do the same with a `prime` version for test purposes.
  #
  # This code will NEVER run in the cloud CI/CD workflows, as there we do
  # the build and push (into GH test registry) once in `builds`, before
  # the strategy matrix blows up the number of executables

  create_builder

  echo "${bright}Building operator from current worktree${reset}"

  CONTROLLER_IMG="$(print_image)"
  make -C "${ROOT_DIR}" CONTROLLER_IMG="${CONTROLLER_IMG}" insecure="true" \
    ARCH="${ARCH}" BUILDER_NAME=${builder_name} docker-build

  echo "${bright}Loading new operator image on cluster ${CLUSTER_NAME}${reset}"

  echo "${bright}Done loading new operator image on cluster ${CLUSTER_NAME}${reset}"

  if [[ "${TEST_UPGRADE_TO_V1}" != "false" ]]; then
    # In order to test the case of upgrading from the current operator
    # to a future one, we build and push an image with a different VERSION
    # to force a different hash for the manager binary.
    # (Otherwise the ONLINE upgrade won't trigger)

    echo "${bright}Building a 'prime' operator from current worktree${reset}"

    PRIME_CONTROLLER_IMG="${CONTROLLER_IMG}-prime"
    CURRENT_VERSION=$(make -C "${ROOT_DIR}" -s print-version)
    PRIME_VERSION="${CURRENT_VERSION}-prime"
    make -C "${ROOT_DIR}" CONTROLLER_IMG="${PRIME_CONTROLLER_IMG}" VERSION="${PRIME_VERSION}" insecure="true" \
      ARCH="${ARCH}" BUILDER_NAME="${builder_name}" docker-build

    echo "${bright}Done loading new 'prime' operator image on cluster ${CLUSTER_NAME}${reset}"
  fi

  docker buildx rm "${builder_name}"
}

deploy() {
  CONTROLLER_IMG="$(print_image)"

  echo "${bright}Deploying manifests from current worktree on cluster ${CLUSTER_NAME}${reset}"

  deploy_operator

  echo "${bright}Done deploying manifests from current worktree on cluster ${CLUSTER_NAME}${reset}"
}

print_image() {
  echo "${registry_name}:5000/cloudnative-pg-testing:latest"
}

export_logs() {
  echo "${bright}Exporting logs from cluster ${CLUSTER_NAME} to ${LOG_DIR}${reset}"

  "export_logs_kind" "${CLUSTER_NAME}"

  echo "${bright}Done exporting logs from cluster ${CLUSTER_NAME} to ${LOG_DIR}${reset}"
}

destroy() {
  echo "${bright}Destroying kind cluster ${CLUSTER_NAME}${reset}"

  "destroy_kind" "${CLUSTER_NAME}"

  echo "${bright}Done destroying kind cluster ${CLUSTER_NAME}${reset}"
}

pyroscope() {
  echo "${bright} Deploying Pyroscope${reset}"
  deploy_pyroscope
  echo "${bright} Done deploying Pyroscope${reset}"
}

##
## MAIN
##

main() {
  if ! getopt -T > /dev/null; then
    # GNU enhanced getopt is available
    parsed_opts=$(getopt -o e:k:n:r -l "engine:,k8s-version:,nodes:,registry" -- "$@") || usage
  else
    # Original getopt is available
    parsed_opts=$(getopt e:k:n:r "$@") || usage
  fi
  eval "set -- $parsed_opts"
  for o; do
    case "${o}" in
    -e | --engine)
      shift
      # no-op, kept for compatibility
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
    -n | --nodes)
      shift
      NODES="${1}"
      shift
      if ! [[ $NODES =~ ^[1-9][0-9]*$ ]]; then
        echo "ERROR: $NODES is not a positive integer!" >&2
        echo >&2
        usage
      fi
      ;;
    -r | --registry)
      shift
      # no-op, kept for compatibility
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

  if [ -z "${K8S_VERSION}" ]; then
    K8S_VERSION=${KIND_NODE_DEFAULT_VERSION}
  fi
  KUBECTL_VERSION=${KUBECTL_VERSION:-$K8S_VERSION}

  # Only here the K8S_VERSION variable contains its final value
  # so we can set the default cluster name
  CLUSTER_NAME=${CLUSTER_NAME:-pg-operator-e2e-${K8S_VERSION//./-}}

  while [ "$#" -gt 0 ]; do
    command=$1
    shift

    # Invoke the command
    case "$command" in

    create | load | load-helper-images | deploy | print-image | export-logs | destroy | pyroscope)
      ensure_registry
      "${command//-/_}"
      ;;
    *)
      echo "ERROR: unknown command ${command}" >&2
      echo >&2
      usage
      ;;
    esac
  done
}

main "$@"
