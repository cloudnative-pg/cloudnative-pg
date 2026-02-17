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

# shellcheck disable=SC2317
# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
HACK_DIR="${ROOT_DIR}/hack"
E2E_DIR="${HACK_DIR}/e2e"

# Specify which engine to use to create the kubernetes cluster.
# E.g.: CLUSTER_ENGINE=k3d ./hack/e2e/run-e2e-local.sh
# Default: kind
export CLUSTER_ENGINE="${CLUSTER_ENGINE:-kind}"

export PRESERVE_CLUSTER=${PRESERVE_CLUSTER:-false}
export BUILD_IMAGE=${BUILD_IMAGE:-false}
export LOG_DIR=${LOG_DIR:-$ROOT_DIR/_logs/}
export ENABLE_APISERVER_AUDIT=${ENABLE_APISERVER_AUDIT:-false}
export POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export PGBOUNCER_IMG=${PGBOUNCER_IMG:-$(grep 'DefaultPgbouncerImage.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}
export CONTROLLER_IMG_DIGEST=${CONTROLLER_IMG_DIGEST:-""}
export CONTROLLER_IMG_PRIME_DIGEST=${CONTROLLER_IMG_PRIME_DIGEST:-""}

export DOCKER_REGISTRY_MIRROR=${DOCKER_REGISTRY_MIRROR:-}
export TEST_CLOUD_VENDOR="local"

# shellcheck disable=SC2329
cleanup() {
  if [ "${PRESERVE_CLUSTER}" = false ]; then
    "${HACK_DIR}/setup-cluster.sh" -e "${CLUSTER_ENGINE}" destroy || true
  else
    set +x
    echo "You've chosen to preserve the Kubernetes cluster."
    echo "You can delete it manually later running:"
    echo "'${HACK_DIR}/setup-cluster.sh' -e ${CLUSTER_ENGINE} destroy"
  fi
}

# Detect the default storage class from the live cluster
detect_default_storage_class() {
  local sc_names sc_count
  sc_names=$(kubectl get storageclass -o json | \
    jq -r '[.items[] | select(.metadata.annotations["storageclass.kubernetes.io/is-default-class"] == "true") | .metadata.name]')
  sc_count=$(echo "$sc_names" | jq 'length')
  if [[ "$sc_count" -eq 0 ]]; then
    echo "ERROR: no default storage class found in the cluster" >&2
    return 1
  elif [[ "$sc_count" -gt 1 ]]; then
    echo "ERROR: multiple default storage classes found: $(echo "$sc_names" | jq -r 'join(", ")')" >&2
    return 1
  fi
  echo "$sc_names" | jq -r '.[0]'
}

# Detect the CSI storage class that supports snapshots (has the default-snapshot-class annotation)
detect_csi_storage_class() {
  local sc_names sc_count
  sc_names=$(kubectl get storageclass -o json | \
    jq -r '[.items[] | select(.metadata.annotations["storage.kubernetes.io/default-snapshot-class"] != null) | .metadata.name]')
  sc_count=$(echo "$sc_names" | jq 'length')
  if [[ "$sc_count" -eq 0 ]]; then
    echo "ERROR: no storage class with default-snapshot-class annotation found in the cluster" >&2
    return 1
  elif [[ "$sc_count" -gt 1 ]]; then
    echo "ERROR: multiple storage classes with default-snapshot-class annotation found: $(echo "$sc_names" | jq -r 'join(", ")')" >&2
    return 1
  fi
  echo "$sc_names" | jq -r '.[0]'
}

# Detect the default volume snapshot class from the CSI storage class annotation
detect_default_volumesnapshot_class() {
  local storage_class=$1 snap_class
  snap_class=$(kubectl get storageclass "$storage_class" -o json | \
    jq -r '.metadata.annotations["storage.kubernetes.io/default-snapshot-class"] // empty')
  if [[ -z "$snap_class" ]]; then
    echo "ERROR: no default-snapshot-class annotation found on storage class ${storage_class}" >&2
    return 1
  fi
  echo "$snap_class"
}

main() {
  # Call to setup-cluster.sh script
  "${HACK_DIR}/setup-cluster.sh" -e "${CLUSTER_ENGINE}" create

  # Detect storage class defaults from the live cluster
  if [[ -z "${E2E_DEFAULT_STORAGE_CLASS:-}" ]]; then
    E2E_DEFAULT_STORAGE_CLASS=$(detect_default_storage_class)
  fi
  export E2E_DEFAULT_STORAGE_CLASS

  if [[ -z "${E2E_CSI_STORAGE_CLASS:-}" ]]; then
    E2E_CSI_STORAGE_CLASS=$(detect_csi_storage_class)
  fi
  export E2E_CSI_STORAGE_CLASS

  if [[ -z "${E2E_DEFAULT_VOLUMESNAPSHOT_CLASS:-}" ]]; then
    E2E_DEFAULT_VOLUMESNAPSHOT_CLASS=$(detect_default_volumesnapshot_class "$E2E_CSI_STORAGE_CLASS")
  fi
  export E2E_DEFAULT_VOLUMESNAPSHOT_CLASS

  trap cleanup EXIT

  # In case image building is forced it will use a default
  # controller image name: cloudnative-pg:e2e.
  # Otherwise it will download the image from docker
  # registry using below credentials.
  if [ "${BUILD_IMAGE}" == false ]; then
    # Prevent e2e tests to proceed with empty tag which
    # will be considered as "latest".
    # This will fail in case heuristic IMAGE_TAG will
    # be empty, and will continue if CONTROLLER_IMG
    # is manually specified during execution, i.e.:
    #
    #     BUILD_IMAGE=false CONTROLLER_IMG=cloudnative-pg:e2e ./hack/e2e/run-e2e-local.sh
    #
    if [ -z "${CONTROLLER_IMG:-}" ]; then
      IMAGE_TAG="$( (git symbolic-ref -q --short HEAD || git describe --tags --exact-match) | tr / -)"
      export CONTROLLER_IMG="ghcr.io/cloudnative-pg/cloudnative-pg-testing:${IMAGE_TAG}"
    fi
  else
    unset CONTROLLER_IMG
    "${HACK_DIR}/setup-cluster.sh" -e "${CLUSTER_ENGINE}" load
  fi

  # Comment out when the a new release of kindest/node is release newer than v1.32.1
  # "${HACK_DIR}/setup-cluster.sh" load-helper-images

  RC=0

  # Run E2E tests
  "${E2E_DIR}/run-e2e.sh" || RC=$?

  ## Export logs
  "${HACK_DIR}/setup-cluster.sh" -e "${CLUSTER_ENGINE}" export-logs

  exit $RC
}

main
