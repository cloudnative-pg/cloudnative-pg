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

# This file contains functions for interacting with the Kubernetes API using $K8S_CLI.

# wait_for(type, namespace, name, interval, retries)
# Waits until a specified Kubernetes object exists.
function wait_for() {
  local type=$1
  local namespace=$2
  local name=$3
  local interval=$4
  local retries=$5

  ITER=0
  while ! ${K8S_CLI} get -n "$namespace" "$type" "$name" && [ "$ITER" -lt "$retries" ]; do
    ITER=$((ITER + 1))
    echo "$name $type doesn't exist yet. Waiting $interval seconds ($ITER of $retries)."
    sleep "$interval"
  done
  # Returns non-zero if the object was not found within retries
  [[ $ITER -lt $retries ]]
}

# retry N command
# Retries a command up to a specific number of times with exponential backoff.
function retry {
  local retries=$1
  shift

  local count=0
  until "$@"; do
    local exit=$?
    local wait=$((2 ** count))
    count=$((count + 1))
    if [ "$count" -lt "$retries" ]; then
      echo "Retry $count/$retries exited $exit, retrying in $wait seconds..." >&2
      sleep "$wait"
    else
      echo "Retry $count/$retries exited $exit, no more retries left." >&2
      return "$exit"
    fi
  done
  return 0
}

# get_default_storage_class detects the default K8s storage class
function get_default_storage_class() {
    ${K8S_CLI} get storageclass -o json | jq -r 'first(.items[] | select (.metadata.annotations["storageclass.kubernetes.io/is-default-class"] == "true") | .metadata.name)'
}

# get_default_snapshot_class detects the snapshot class for a given storage class
function get_default_snapshot_class() {
    local STORAGE_CLASS=${1:-${1:?STORAGE_CLASS is required}}
    ${K8S_CLI} get storageclass "$STORAGE_CLASS" -o json | jq -r '.metadata.annotations["storage.kubernetes.io/default-snapshot-class"]'
}

function deploy_prometheus_crds() {
  # Requires helm to be installed and $K8S_CLI (kubectl/oc) to be functional.
  # shellcheck disable=SC2154
  echo -e "${bright}Starting deployment of Prometheus CRDs...${reset}"

  # 1. Add Prometheus Community Helm Repository
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts

  # 2. Install only the CRDs required by the Prometheus operator
  # We install into kube-system as that namespace is standard and always exists.
  retry 3 helm -n kube-system install prometheus-operator-crds prometheus-community/prometheus-operator-crds

  echo -e "${bright}Prometheus CRDs deployed.${reset}"
}

function deploy_pyroscope() {
  # Requires helm to be installed and available in the environment.

  echo -e "${bright}Deploying Pyroscope and enabling pprof profiling on the operator...${reset}"

  # 1. Add Pyroscope Helm Repository
  helm repo add pyroscope-io https://grafana.github.io/helm-charts

  # 2. Define Pyroscope configuration values and install via Helm
  local values_file="${TEMP_DIR}/pyroscope_values.yaml"
  cat >"${values_file}" <<-EOF
pyroscopeConfigs:
  log-level: "debug"
EOF
  helm upgrade --install --create-namespace -n pyroscope pyroscope pyroscope-io/pyroscope -f "${values_file}"

  # 3. Create patch file to enable operator profiling annotations
  # These annotations tell Pyroscope's agent what ports and profiles to scrape.
  local annotations="${TEMP_DIR}/pyroscope_annotations.yaml"
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

  # Patch the operator deployment to enable profiling ports/scrapes
  "${K8S_CLI}" -n cnpg-system patch deployment cnpg-controller-manager --patch-file "${annotations}"

  # 4. Patch the operator's ConfigMap to inherit these new annotations
  # This ensures PostgreSQL clusters managed by the operator also inherit the profiling settings.
  local configMaps="${TEMP_DIR}/cnpg_configmap_config.yaml"
  cat >"${configMaps}" <<-EOF
data:
  INHERITED_ANNOTATIONS: "profiles.grafana.com/*"
EOF

  # Find the name of the ConfigMap currently referenced by the operator deployment
  local configMapName
  configMapName=$("${K8S_CLI}" -n cnpg-system get deployments.apps cnpg-controller-manager -o jsonpath='{.spec.template.spec.containers[0].envFrom[0].configMapRef.name}')

  # Patch the ConfigMap
  "${K8S_CLI}" -n cnpg-system patch configmap "${configMapName}" --patch-file "${configMaps}"

  echo -e "${bright}Pyroscope deployment successful and operator patched to expose profiling data.${reset}"
}

# print_operator_image: prints the operator image reference (as set on the
# controller-manager Deployment's first container) when the deployment exists.
function print_operator_image() {
    local image
    image=$(${K8S_CLI} get deployment cnpg-controller-manager -n cnpg-system \
        --ignore-not-found \
        -o jsonpath='{.spec.template.spec.containers[0].image}')
    if [[ -n "${image}" ]]; then
        printf '%bOperator image: %s%b\n' "${bright}" "${image}" "${reset}"
    fi
}

# reset_operator_namespace: deletes the cnpg-system namespace if present and
# waits for finalization, so the next apply doesn't race a terminating namespace.
function reset_operator_namespace() {
    if ${K8S_CLI} get ns cnpg-system >/dev/null 2>&1; then
        ${K8S_CLI} delete ns cnpg-system --ignore-not-found --wait=false
        ${K8S_CLI} wait --for=delete ns/cnpg-system --timeout=60s
    fi
}

# wait_operator_ready: waits for the operator deployment rollout to finish and
# prints a completion banner. Accepts an optional deployment name.
# When installed via manifest or the cnpg plugin the deployment is called
# cnpg-controller-manager; when installed via Helm it is called
# cnpg-cloudnative-pg. See:
# https://cloudnative-pg.io/docs/1.29/installation_upgrade#using-the-helm-chart
function wait_operator_ready() {
    local deploy_name="${1:-cnpg-controller-manager}"
    ${K8S_CLI} -n cnpg-system rollout status deploy/"${deploy_name}" --timeout=5m
    printf '%bOperator deployment complete.%b\n' "${bright}" "${reset}"
    print_operator_image
}

# deploy_operator_from_manifest <operator>
# Deploys the operator by applying its manifest. The <operator> argument is
# interpreted either as a semver version (e.g. 1.28.1 or v1.28.1, with optional
# prerelease suffix), in which case the published release manifest from the main
# repository is used, or as a branch name (e.g. main, release-1.28), in which
# case the snapshot manifest from the cloudnative-pg/artifacts repository is
# used.
function deploy_operator_from_manifest() {
    local operator="${1:?operator is required}"
    local mode
    local manifest_url

    if [[ "${operator}" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$ ]]; then
        local version="${operator#v}"
        mode="version"
        manifest_url="https://github.com/cloudnative-pg/cloudnative-pg/releases/download/v${version}/cnpg-${version}.yaml"
    elif [[ "${operator}" =~ ^v?[0-9] ]]; then
        # Looks version-like but isn't valid semver -- refuse rather than silently
        # fall through to branch mode and produce a misleading "not found" error.
        printf '%bError: %s is not a valid operator version (expected e.g. 1.28.1 or v1.28.1)%b\n' \
            "${bright}" "${operator}" "${reset}" >&2
        exit 1
    elif [[ "${operator}" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ ]] && [[ "${operator}" != *..* ]]; then
        mode="branch"
        manifest_url="https://raw.githubusercontent.com/cloudnative-pg/artifacts/${operator}/manifests/operator-manifest.yaml"
    else
        printf '%bError: %s is not a valid operator value%b\n' \
            "${bright}" "${operator}" "${reset}" >&2
        printf '%bExpected a semver (e.g. 1.28.1) or a branch name (e.g. main, release-1.28).%b\n' \
            "${bright}" "${reset}" >&2
        exit 1
    fi

    local manifest_file="${TEMP_DIR}/cnpg-operator-manifest.yaml"
    if ! curl -fsSL --retry 5 --retry-delay 2 -o "${manifest_file}" "${manifest_url}"; then
        printf '%bError: Manifest not found at %s%b\n' "${bright}" "${manifest_url}" "${reset}" >&2
        printf '%bInterpreted %s as a %s.%b\n' "${bright}" "${operator}" "${mode}" "${reset}" >&2
        exit 1
    fi

    printf '%bDeploying operator from %s%b\n' "${bright}" "${operator}" "${reset}"
    reset_operator_namespace
    # --server-side avoids the last-applied-configuration annotation exceeding
    # the 262144 byte limit on large CRDs; --force-conflicts lets us adopt
    # existing field ownership when re-deploying or switching operator version.
    ${K8S_CLI} apply --server-side --force-conflicts -f "${manifest_file}"
    wait_operator_ready
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
  local plugin_file="${TEMP_DIR}/csi-hostpath-plugin.yaml"
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
  "${K8S_CLI}" annotate storageclass csi-hostpath-sc storage.kubernetes.io/default-snapshot-class=csi-hostpath-snapclass

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
  "load_image_${CLUSTER_ENGINE}" "${CLUSTER_NAME}" "${FLUENTD_LOCAL_IMAGE}"

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

# deploy_operator_from_source: applies the pre-generated operator manifest.
# Requires generate_operator_manifest (via setup-cluster.sh) to have been
# called first.
function deploy_operator_from_source() {
    # shellcheck disable=SC2154
    echo -e "${bright}Deploying CNPG operator from ${OPERATOR_MANIFEST_PATH}${reset}"
    ${K8S_CLI} apply --server-side --force-conflicts -f "${OPERATOR_MANIFEST_PATH}"
    wait_operator_ready
}

# deploy_operator_with_helm: installs the operator from the published CNPG Helm
# chart, overriding the chart's default image with the locally-built one so the
# same binary under test is used. Only supported with OPERATOR=local.
#
# CRDs are applied from the local source tree (config/helm) so they always
# match the locally-built operator binary under test. The chart's built-in CRD
# management is disabled via --set crds.create=false.
function deploy_operator_with_helm() {
    if ! command -v helm &>/dev/null; then
        echo "ERROR: 'helm' not found in PATH. Install Helm before running with CNPG_DEPLOYMENT_METHOD=helm." >&2
        exit 1
    fi

    echo -e "${bright}Deploying CNPG operator via Helm chart...${reset}"

    # Apply CRDs from the local source tree to ensure they match the operator binary under test.
    ${K8S_CLI} apply --server-side -k "${ROOT_DIR}/config/helm"

    helm repo add cnpg https://cloudnative-pg.github.io/charts
    helm repo update cnpg

    local -a helm_args=(
        --namespace cnpg-system
        --create-namespace
        --set crds.create=false
        --set config.create=false
        --set "additionalArgs[0]=--secret-name=cnpg-controller-manager-config"
    )

    # Override the chart's default image with the locally-built one.
    # Currently OPERATOR=local is the only supported value for Helm deployments,
    # but this guard is kept explicit to ease future extension.
    # shellcheck disable=SC2153
    if [[ "${OPERATOR}" == "local" ]]; then
        helm_args+=(
            --set "image.repository=${CONTROLLER_IMG%:*}"
            --set "image.tag=${CONTROLLER_IMG##*:}"
            --set "additionalEnv[0].name=POSTGRES_IMAGE_NAME"
            --set "additionalEnv[0].value=${POSTGRES_IMG}"
            --set "additionalEnv[1].name=PGBOUNCER_IMAGE_NAME"
            --set "additionalEnv[1].value=${PGBOUNCER_IMG}"
        )
    fi

    helm upgrade --install cnpg cnpg/cloudnative-pg "${helm_args[@]}"
    # When installed via Helm the deployment is called cnpg-cloudnative-pg, not
    # cnpg-controller-manager. See:
    # https://cloudnative-pg.io/docs/current/installation_upgrade#using-the-helm-chart
    wait_operator_ready "cnpg-cloudnative-pg"
}
