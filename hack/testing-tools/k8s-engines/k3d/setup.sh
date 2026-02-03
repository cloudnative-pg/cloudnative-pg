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

# shellcheck disable=SC1090,SC1091

# Kind-specific cluster creation logic.
set -eEuo pipefail

# Load common modules needed for paths, config, and registry helpers
DIR="$(dirname "${BASH_SOURCE[0]}")"
COMMON_DIR="${DIR}/../../common"
source "${COMMON_DIR}/00-paths.sh"
source "${COMMON_DIR}/10-config.sh"
source "${COMMON_DIR}/20-utils-k8s.sh"
source "${COMMON_DIR}/40-utils-registry.sh"

# --- KIND SPECIFIC CONSTANTS AND DEFAULTS ---
K8S_VERSION=${K8S_VERSION:-$K3D_NODE_DEFAULT_VERSION}
NODES=${NODES:-3}
ENABLE_APISERVER_AUDIT=${ENABLE_APISERVER_AUDIT:-}
ENABLE_FLUENTD=${ENABLE_FLUENTD:-false}

TEMP_DIR_LOCAL="$(mktemp -d)"
trap 'rm -fr ${TEMP_DIR_LOCAL}' EXIT
# --------------------------------------------------------

# --- KIND HELPER FUNCTIONS ---

# load_image_k3d: Loads a Docker image directly into the Kind cluster nodes.
function load_image_k3d() {
  local cluster_name=$1
  local image=$2
  k3d image import "${image}" -c "${cluster_name}"
}

# deploy_csi_host_path: Deploys the host path CSI driver and snapshotter components.
function deploy_csi_host_path() {
  # shellcheck disable=SC2154
  echo -e "${bright}Deploying CSI Host Path Driver...${reset}"

  # Base URL for CSI repository manifests
  local CSI_BASE_URL=https://raw.githubusercontent.com/kubernetes-csi

  # --- 1. Install External Snapshotter CRDs and Controller (Versions sourced from 10-config.sh) ---

  ## Apply CRDs
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/client/config/crd/snapshot.storage.k8s.io_volumesnapshotclasses.yaml
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/client/config/crd/snapshot.storage.k8s.io_volumesnapshotcontents.yaml
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/client/config/crd/snapshot.storage.k8s.io_volumesnapshots.yaml

  ## Apply RBAC and Controller
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/deploy/kubernetes/snapshot-controller/rbac-snapshot-controller.yaml
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/deploy/kubernetes/snapshot-controller/setup-snapshot-controller.yaml
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-snapshotter/"${EXTERNAL_SNAPSHOTTER_VERSION}"/deploy/kubernetes/csi-snapshotter/rbac-csi-snapshotter.yaml

  # --- 2. Install External Sidecar Components ---

  ## Install external provisioner
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-provisioner/"${EXTERNAL_PROVISIONER_VERSION}"/deploy/kubernetes/rbac.yaml

  ## Install external attacher
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-attacher/"${EXTERNAL_ATTACHER_VERSION}"/deploy/kubernetes/rbac.yaml

  ## Install external resizer
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/external-resizer/"${EXTERNAL_RESIZER_VERSION}"/deploy/kubernetes/rbac.yaml

  # --- 3. Install Driver and Plugin ---

  ## Create a temporary file for the modified plugin deployment. This updates the image tag.
  local plugin_file="${TEMP_DIR_LOCAL}/csi-hostpath-plugin.yaml"
  curl -sSL "${CSI_BASE_URL}/csi-driver-host-path/${CSI_DRIVER_HOST_PATH_VERSION}/deploy/kubernetes-1.30/hostpath/csi-hostpath-plugin.yaml" |
    sed "s|registry.k8s.io/sig-storage/hostpathplugin:.*|registry.k8s.io/sig-storage/hostpathplugin:${CSI_DRIVER_HOST_PATH_VERSION}|g" > "${plugin_file}"

  # Apply driver info and plugin deployment
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/csi-driver-host-path/"${CSI_DRIVER_HOST_PATH_VERSION}"/deploy/kubernetes-1.30/hostpath/csi-hostpath-driverinfo.yaml
  "${K8S_CLI}" apply -f "${plugin_file}"
  rm "${plugin_file}"

  # --- 4. Configure Storage Classes ---

  ## Create VolumeSnapshotClass
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/csi-driver-host-path/"${CSI_DRIVER_HOST_PATH_VERSION}"/deploy/kubernetes-1.30/hostpath/csi-hostpath-snapshotclass.yaml

  ## Patch VolumeSnapshotClass to allow snapshots of running PostgreSQL instances
  ## by ignoring read failures during snapshot creation
  "${K8S_CLI}" patch volumesnapshotclass csi-hostpath-snapclass -p '{"parameters":{"ignoreFailedRead":"true"}}' --type merge

  ## Create StorageClass
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/csi-driver-host-path/"${CSI_DRIVER_HOST_PATH_VERSION}"/examples/csi-storageclass.yaml

  ## Annotate the StorageClass to set the default snapshot class
  "${K8S_CLI}" annotate storageclass csi-hostpath-sc storage.kubernetes.io/default-snapshot-class=csi-hostpath-snapshotclass

  # Wait for CSI plugin to be ready
  echo -e "${bright}CSI driver plugin deployment has started. Waiting for the CSI plugin to be ready...${reset}"
  local ITER=0
  while true; do
    if [[ $ITER -ge 300 ]]; then
      echo -e "${bright}Timeout: The CSI plugin did not become ready within the expected time.${reset}"
      exit 1
    fi
    local NUM_SPEC
    local NUM_STATUS
    NUM_SPEC=$("${K8S_CLI}" get statefulset csi-hostpathplugin -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "")
    NUM_STATUS=$("${K8S_CLI}" get statefulset csi-hostpathplugin -o jsonpath='{.status.availableReplicas}' 2>/dev/null || echo "")
    if [[ -n "$NUM_SPEC" && "$NUM_SPEC" == "$NUM_STATUS" ]]; then
      echo -e "${bright}Success: The CSI plugin is deployed and ready.${reset}"
      break
    fi
    sleep 1
    ((++ITER))
  done
}

# deploy_fluentd: Pulls the FluentD image and deploys the DaemonSet.
function deploy_fluentd() {
  local FLUENTD_IMAGE=fluent/fluentd-kubernetes-daemonset:v1.14.3-debian-forward-1.0
  # shellcheck disable=SC2154
  local FLUENTD_LOCAL_IMAGE="${registry_name}:5000/fluentd-kubernetes-daemonset:local"

  echo -e "${bright}Starting FluentD deployment...${reset}"
  docker pull "${FLUENTD_IMAGE}"
  docker tag "${FLUENTD_IMAGE}" "${FLUENTD_LOCAL_IMAGE}"
  # shellcheck disable=SC2153
  load_image_kind "${CLUSTER_NAME}" "${FLUENTD_LOCAL_IMAGE}"

  "${K8S_CLI}" apply -f "${E2E_DIR}/local-fluentd.yaml"

  # Wait for FluentD to be ready
  echo -e "${bright}Waiting for FluentD to become ready...${reset}"
  local ITER=0
  local NODE
  NODE=$("${K8S_CLI}" get nodes --no-headers | wc -l | tr -d " ")
  while true; do
    if [[ $ITER -ge 300 ]]; then
      echo -e "${bright}Time out waiting for FluentD readiness${reset}"
      exit 1
    fi
    local NUM_READY
    NUM_READY=$("${K8S_CLI}" get ds fluentd -n kube-system -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "")
    if [[ -n "$NUM_READY" && "$NUM_READY" == "$NODE" ]]; then
      echo -e "${bright}FluentD is Ready${reset}"
      break
    fi
    sleep 1
    ((++ITER))
  done
}

# create_cluster_kind: Generates the config file and creates the Kind cluster.
function create_cluster_k3d() {
  local k8s_version=$1
  local cluster_name=$2

  local latest_k3s_tag
  latest_k3s_tag=$(k3d version list k3s | grep -- "^${k8s_version//./\\.}"'\+-k3s[0-9]$' | tail -n 1)

  local options=()
  local config_file="${TEMP_DIR}/k3d-registries.yaml"
  cat >"${config_file}" <<-EOF
mirrors:
  "${registry_name}:5000":
    endpoint:
    - http://${registry_name}:5000
EOF

if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
  cat >>"${config_file}" <<-EOF
  "docker.io":
    endpoint:
      - "${DOCKER_REGISTRY_MIRROR}"
EOF
fi

options+=(--registry-config "${config_file}")

  local agents=()
  if [ "$NODES" -gt 1 ]; then
    agents=(-a "${NODES}")
  fi

  K3D_FIX_MOUNTS=1 k3d cluster create "${options[@]}" "${agents[@]}" -i "rancher/k3s:${latest_k3s_tag}" --no-lb "${cluster_name}" \
    --k3s-arg "--disable=traefik@server:0" --k3s-arg "--disable=metrics-server@server:0" \
    --k3s-arg "--node-taint=node-role.kubernetes.io/master:NoSchedule@server:0" #wokeignore:rule=master

  docker network connect "k3d-${cluster_name}" "${registry_name}" &>/dev/null || true
}
# ---------------------------------------------------------------------------------

# --- MAIN EXECUTION ---

main() {
  # Validate required tools are installed
  validate_required_tools k3d docker kubectl helm

  echo -e "${bright}Running Kind setup: Creating cluster ${CLUSTER_NAME} with version ${K8S_VERSION}${reset}"

  create_cluster_k3d "${K8S_VERSION}" "${CLUSTER_NAME}"

  # Support for docker:dind service
  if [ "${DOCKER_HOST:-}" == "tcp://docker:2376" ]; then
    sed -i -E -e 's/0\.0\.0\.0/docker/g' "${HOME}/.kube/config"
  fi

  # Deploy optional and required add-ons
  if [ "${ENABLE_FLUENTD}" = "true" ]; then
    deploy_fluentd
  fi
  if [ "${ENABLE_CSI_DRIVER:-true}" = "true" ]; then
    deploy_csi_host_path
  fi
  deploy_prometheus_crds

  echo -e "${bright}Kind cluster ${CLUSTER_NAME} setup complete.${reset}"
}

main
