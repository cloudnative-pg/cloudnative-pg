#!/usr/bin/env bash
#
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-20.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
#

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
  while ! ${K8S_CLI} get -n "$namespace" "$type" "$name" && [ $ITER -lt "$retries" ]; do
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
    if [ $count -lt "$retries" ]; then
      echo "Retry $count/$retries exited $exit, retrying in $wait seconds..." >&2
      sleep $wait
    else
      echo "Retry $count/$retries exited $exit, no more retries left." >&2
      return $exit
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
  echo "Starting deployment of Prometheus CRDs..."

  # 1. Add Prometheus Community Helm Repository
  helm repo add prometheus-community https://prometheus-community.github.io/helm-charts

  # 2. Install only the CRDs required by the Prometheus operator
  # We install into kube-system as that namespace is standard and always exists.
  helm -n kube-system install prometheus-operator-crds prometheus-community/prometheus-operator-crds

  echo "Prometheus CRDs deployed."
}

function deploy_pyroscope() {
  # Requires helm to be installed and available in the environment.

  echo "Deploying Pyroscope and enabling pprof profiling on the operator..."

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

  echo "Pyroscope deployment successful and operator patched to expose profiling data."
}
