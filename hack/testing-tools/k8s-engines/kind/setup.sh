#!/usr/bin/env bash
#
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
#

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
K8S_VERSION=${K8S_VERSION:-$KIND_NODE_DEFAULT_VERSION}
NODES=${NODES:-3}
ENABLE_APISERVER_AUDIT=${ENABLE_APISERVER_AUDIT:-}
ENABLE_FLUENTD=${ENABLE_FLUENTD:-false} 

TEMP_DIR_LOCAL="$(mktemp -d)"
trap 'rm -fr ${TEMP_DIR_LOCAL}' EXIT
# --------------------------------------------------------

# --- KIND HELPER FUNCTIONS ---

# load_image_kind: Loads a Docker image directly into the Kind cluster nodes.
function load_image_kind() {
  local cluster_name=$1
  local image=$2
  kind load -v 1 docker-image --name "${cluster_name}" "${image}"
}

# deploy_csi_host_path: Deploys the host path CSI driver and snapshotter components.
function deploy_csi_host_path() {
  echo "Deploying CSI Host Path Driver..."
  
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

  ## Patch VolumeSnapshotClass to ignore failures (crucial for PostgreSQL testing)
  "${K8S_CLI}" patch volumesnapshotclass csi-hostpath-snapclass -p '{"parameters":{"ignoreFailedRead":"true"}}' --type merge

  ## Create StorageClass
  "${K8S_CLI}" apply -f "${CSI_BASE_URL}"/csi-driver-host-path/"${CSI_DRIVER_HOST_PATH_VERSION}"/examples/csi-storageclass.yaml
  
  ## Annotate the StorageClass to set the default snapshot class
  "${K8S_CLI}" annotate storageclass csi-hostpath-sc storage.kubernetes.io/default-snapshot-class=csi-hostpath-snapshotclass

  echo "CSI plugin deployment initiated. (Requires wait loop from runner script)."
}

# deploy_fluentd: Pulls the FluentD image and deploys the DaemonSet.
function deploy_fluentd() {
  local FLUENTD_IMAGE=fluent/fluentd-kubernetes-daemonset:v1.14.3-debian-forward-1.0
  local FLUENTD_LOCAL_IMAGE="${registry_name}:5000/fluentd-kubernetes-daemonset:local"
  
  echo "Starting FluentD deployment..."
  docker pull "${FLUENTD_IMAGE}"
  docker tag "${FLUENTD_IMAGE}" "${FLUENTD_LOCAL_IMAGE}"
  load_image_kind "${CLUSTER_NAME}" "${FLUENTD_LOCAL_IMAGE}"

  "${K8S_CLI}" apply -f "${E2E_DIR}/local-fluentd.yaml"
  # NOTE: The wait loop for FluentD readiness is typically placed here.
  echo "FluentD deployment initiated."
}

# create_cluster_kind: Generates the config file and creates the Kind cluster.
function create_cluster_kind() {
  local k8s_version=$1
  local cluster_name=$2

  # Create kind config (Configuration logic copied from old setup-cluster.sh)
  config_file="${TEMP_DIR_LOCAL}/kind-config.yaml"
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
    echo "Kind configuration file:"
    cat "${config_file}"
  fi

  # Create the cluster
  echo "Generating Kind configuration and running 'kind create cluster'..."
  kind create cluster --name "${cluster_name}" --image "kindest/node:${k8s_version}" --config "${config_file}"

  docker network connect "kind" "${registry_name}" &>/dev/null || true

  # Configure registry mirrors using hosts.toml files
  REGISTRY_DIR="/etc/containerd/certs.d/${registry_name}:5000"
  for node in $(kind get nodes --name "${cluster_name}"); do
    docker exec "$node" mkdir -p "${REGISTRY_DIR}"
    docker exec "$node" bash -c "cat > ${REGISTRY_DIR}/hosts.toml <<EOF
[host.\"http://${registry_name}:5000\"]
EOF
"
  done

  # Configure docker.io mirror if specified
  if [ -n "${DOCKER_REGISTRY_MIRROR:-}" ]; then
    DOCKER_IO_DIR="/etc/containerd/certs.d/docker.io"
    for node in $(kind get nodes --name "${cluster_name}"); do
      docker exec "$node" mkdir -p "${DOCKER_IO_DIR}"
      docker exec "$node" bash -c "cat > ${DOCKER_IO_DIR}/hosts.toml <<EOF
[host.\"${DOCKER_REGISTRY_MIRROR}\"]
EOF
"
    done
  fi

  # Workaround for https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files
  for node in $(kind get nodes --name "${cluster_name}"); do
    docker exec "$node" sysctl fs.inotify.max_user_watches=524288 fs.inotify.max_user_instances=512
  done
}
# ---------------------------------------------------------------------------------

# --- MAIN EXECUTION ---

main() {
  echo "Running Kind setup: Creating cluster ${CLUSTER_NAME} with version ${K8S_VERSION}"

  create_cluster_kind "${K8S_VERSION}" "${CLUSTER_NAME}"

  # Support for docker:dind service
  if [ "${DOCKER_HOST:-}" == "tcp://docker:2376" ]; then
    sed -i -E -e 's/0\.0\.0\.0/docker/g' "${HOME}/.kube/config"
  fi

  # Deploy optional and required add-ons
  deploy_fluentd
  deploy_csi_host_path
  deploy_prometheus_crds

  echo "Kind cluster ${CLUSTER_NAME} setup complete."
}

main
