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

# K3D-specific cluster creation logic.
set -eEuo pipefail

# Load common modules needed for paths, config, and registry helpers
DIR="$(dirname "${BASH_SOURCE[0]}")"
COMMON_DIR="${DIR}/../../common"
source "${COMMON_DIR}/00-paths.sh"
source "${COMMON_DIR}/10-config.sh"
source "${COMMON_DIR}/20-utils-k8s.sh"
source "${COMMON_DIR}/40-utils-registry.sh"

# --- K3D SPECIFIC CONSTANTS AND DEFAULTS ---
K8S_VERSION=${K8S_VERSION:-$K3D_NODE_DEFAULT_VERSION}
NODES=${NODES:-3}
ENABLE_APISERVER_AUDIT=${ENABLE_APISERVER_AUDIT:-}
ENABLE_FLUENTD=${ENABLE_FLUENTD:-false}

TEMP_DIR_LOCAL="$(mktemp -d)"
trap 'rm -fr ${TEMP_DIR_LOCAL}' EXIT
# --------------------------------------------------------

# --- K3D HELPER FUNCTIONS ---

# load_image_k3d: Loads a Docker image directly into the K3D cluster nodes.
function load_image_k3d() {
  local cluster_name=$1
  local image=$2
  k3d image import "${image}" -c "${cluster_name}"
}

# create_cluster_k3d: Generates the config file and creates the K3D cluster.
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

  echo -e "${bright}Running K3D setup: Creating cluster ${CLUSTER_NAME} with version ${K8S_VERSION}${reset}"

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

  echo -e "${bright}K3D cluster ${CLUSTER_NAME} setup complete.${reset}"
}

main
