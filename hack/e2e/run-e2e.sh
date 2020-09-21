#!/usr/bin/env bash

##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
##

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
    set -x
fi

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
CONTROLLER_IMG=${CONTROLLER_IMG:-internal.2ndq.io/k8s/cloud-native-postgresql:latest}
POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}

# Process the e2e templates
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG}.0}
export E2E_DEFAULT_STORAGE_CLASS=${E2E_DEFAULT_STORAGE_CLASS:-standard}
find "${ROOT_DIR}"/tests/*/fixtures -name "*.template" | \
while read -r f; do
  envsubst <"${f}" >"${f%.template}"
done

# Getting the operator images need a pull secret
kubectl create namespace postgresql-operator-system
if [ -n "${DOCKER_SERVER-}" ] && [ -n "${DOCKER_USERNAME-}" ] && [ -n "${DOCKER_PASSWORD-}" ]; then
  kubectl create secret docker-registry \
    -n postgresql-operator-system \
    postgresql-operator-pull-secret \
    --docker-server="${DOCKER_SERVER}" \
    --docker-username="${DOCKER_USERNAME}" \
    --docker-password="${DOCKER_PASSWORD}"
fi

CONTROLLER_IMG="${CONTROLLER_IMG}" \
  POSTGRES_IMAGE_NAME="${POSTGRES_IMG}" \
  make -C "${ROOT_DIR}" deploy
kubectl wait --for=condition=Available --timeout=2m \
  -n postgresql-operator-system deployments \
  postgresql-operator-controller-manager

# Unset DEBUG to prevent k8s from spamming messages
unset DEBUG

# Create at most 4 testing nodes. Using -p instead of --nodes
# would create CPUs-1 nodes and saturate the testing server
ginkgo --nodes=4 --slowSpecThreshold=300 -v "${ROOT_DIR}/tests/e2e/..."

# Performance tests need to run on a single node to avoid concurrency
ginkgo --nodes=1 --slowSpecThreshold=300 -v "${ROOT_DIR}/tests/performance/..."
