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
NODES=${NODES:-3}
ENABLE_APISERVER_AUDIT=${ENABLE_APISERVER_AUDIT:-}
ENABLE_FLUENTD=${ENABLE_FLUENTD:-false}

# --------------------------------------------------------

# --- KIND HELPER FUNCTIONS ---
source "${DIR}/load-helper-images.sh"

# create_cluster_kind: Generates the config file and creates the Kind cluster.
function create_cluster_kind() {
  local k8s_version=$1
  local cluster_name=$2

  # Generate Kind cluster configuration
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

  # Add containerdConfigPatches section to enable hosts-based registry configuration
  cat >>"${config_file}" <<-EOF

containerdConfigPatches:
- |-
  [plugins."io.containerd.cri.v1.images".registry]
    config_path = "/etc/containerd/certs.d"
EOF

  if [ "${DEBUG-}" = true ]; then
    echo -e "${bright}Kind configuration file:${reset}"
    cat "${config_file}"
  fi

  # Create the cluster
  echo -e "${bright}Generating Kind configuration and running 'kind create cluster'...${reset}"
  kind create cluster --name "${cluster_name}" --image "kindest/node:${k8s_version}" --config "${config_file}"

  docker network connect "kind" "${registry_name}" &>/dev/null || true

  # Configure registry mirrors using hosts.toml files
  # Note: Kind nodes access the registry via docker network using the container's
  # internal port (5000), not the host-exposed port (registry_port).
  REGISTRY_DIR="/etc/containerd/certs.d/${registry_name}:5000"
  while IFS= read -r node; do
    docker exec "$node" mkdir -p "${REGISTRY_DIR}"
    docker exec "$node" bash -c "cat > ${REGISTRY_DIR}/hosts.toml <<EOF
[host.\"http://${registry_name}:5000\"]
EOF
"
  done < <(kind get nodes --name "${cluster_name}")

  # Configure docker.io mirror if specified
  if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
    # Validate that DOCKER_REGISTRY_MIRROR is a valid URL
    if [[ ! "${DOCKER_REGISTRY_MIRROR}" =~ ^https?:// ]]; then
      echo "ERROR: DOCKER_REGISTRY_MIRROR must be a valid HTTP(S) URL, got: ${DOCKER_REGISTRY_MIRROR}" >&2
      exit 1
    fi
    DOCKER_IO_DIR="/etc/containerd/certs.d/docker.io"
    while IFS= read -r node; do
      docker exec "$node" mkdir -p "${DOCKER_IO_DIR}"
      docker exec "$node" bash -c "cat > ${DOCKER_IO_DIR}/hosts.toml <<EOF
[host.\"${DOCKER_REGISTRY_MIRROR}\"]
EOF
"
    done < <(kind get nodes --name "${cluster_name}")
  fi

  # Workaround for https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files
  while IFS= read -r node; do
    docker exec "$node" sysctl fs.inotify.max_user_watches=524288 fs.inotify.max_user_instances=512
  done < <(kind get nodes --name "${cluster_name}")
}
# ---------------------------------------------------------------------------------

# --- MAIN EXECUTION ---

main() {
  # Validate required tools are installed
  validate_required_tools kind docker kubectl helm

  echo -e "${bright}Running Kind setup: Creating cluster ${CLUSTER_NAME} with version ${K8S_VERSION}${reset}"

  create_cluster_kind "${K8S_VERSION}" "${CLUSTER_NAME}"

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
