#!/usr/bin/env bash

##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2022 EnterpriseDB Corporation.
##

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
HACK_DIR="${ROOT_DIR}/hack"
E2E_DIR="${HACK_DIR}/e2e"

export PRESERVE_CLUSTER=${PRESERVE_CLUSTER:-false}
export BUILD_IMAGE=${BUILD_IMAGE:-false}
export K8S_VERSION=${K8S_VERSION:-v1.23.1}
export CLUSTER_ENGINE=k3d
export CLUSTER_NAME=pg-operator-e2e-${K8S_VERSION//./-}
export LOG_DIR=${LOG_DIR:-$ROOT_DIR/_logs/}

export POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}
export E2E_DEFAULT_STORAGE_CLASS=${E2E_DEFAULT_STORAGE_CLASS:-local-path}

export DOCKER_REGISTRY_MIRROR=${DOCKER_REGISTRY_MIRROR:-}

cleanup() {
  if [ "${PRESERVE_CLUSTER}" = false ]; then
    "${HACK_DIR}/setup-cluster.sh" destroy || true
  else
    set +x
    echo "You've chosen to preserve the Kubernetes cluster."
    echo "You can delete it manually later running:"
    echo "'${HACK_DIR}/setup-cluster.sh' destroy"
  fi
}

main() {
  # Call to setup-cluster.sh script
  "${HACK_DIR}/setup-cluster.sh" create

  trap cleanup EXIT

  # In case image building is forced it will use a default
  # controller image name: cloud-native-postgresql:e2e.
  # Otherwise it will download the image from docker
  # registry using below credentials.
  if [ "${BUILD_IMAGE}" == false ]; then
    # Prevent e2e tests to proceed with empty tag which
    # will be considered as "latest" (#CNP-289).
    # This will fail in case heuristic IMAGE_TAG will
    # be empty, and will continue if CONTROLLER_IMG
    # is manually specified during execution, i.e.:
    #
    #     BUILD_IMAGE=false CONTROLLER_IMG=cloud-native-postgresql:e2e ./hack/e2e/run-e2e-k3d.sh
    #
    if [ -z "${CONTROLLER_IMG:-}" ]; then
      IMAGE_TAG="$( (git symbolic-ref -q --short HEAD || git describe --tags --exact-match) | tr / -)"
      export CONTROLLER_IMG="quay.io/enterprisedb/cloud-native-postgresql-testing:${IMAGE_TAG}"
    fi
  else
    unset CONTROLLER_IMG
    "${HACK_DIR}/setup-cluster.sh" load
  fi

  RC=0

  # Run E2E tests
  "${E2E_DIR}/run-e2e.sh" || RC=$?

  ## Export logs
  "${HACK_DIR}/setup-cluster.sh" export-logs

  exit $RC
}

main
