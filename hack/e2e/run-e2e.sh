#!/usr/bin/env bash

##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2021 EnterpriseDB Corporation.
##

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
    set -x
fi

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
CONTROLLER_IMG=${CONTROLLER_IMG:-$("${ROOT_DIR}/hack/setup-cluster.sh" print-image)}
TEST_UPGRADE_TO_V1=${TEST_UPGRADE_TO_V1:-true}
POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}

install_go_module() {
  local module=$1
  local GO_TMP_DIR
  GO_TMP_DIR=$(mktemp -d)
  cd "$GO_TMP_DIR"
  go mod init tmp
  go get -u "${module}"
  rm -rf "$GO_TMP_DIR"
  cd -
}

notinpath () {
    case "$PATH" in
        *:$1:* | *:$1 | $1:*)
            return 1
            ;;
        *)
            return 0
            ;;
    esac
}

# Process the e2e templates
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}
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

go_bin="$(go env GOPATH)/bin"
if notinpath "${go_bin}"; then
  export PATH="${go_bin}:${PATH}"
fi

if ! which ginkgo &>/dev/null; then
  install_go_module "github.com/onsi/ginkgo/ginkgo"
fi

if [[ "${TEST_UPGRADE_TO_V1}" != "false" ]]; then
  # Install a version of the operator using v1alpha1
  kubectl apply -f "${ROOT_DIR}/releases/postgresql-operator-0.7.0.yaml"

  # Generate a manifest for the operator after the api upgrade
  # TODO: this is almost a "make deploy". Refactor.
  make manifests kustomize
  KUSTOMIZE="${ROOT_DIR}/bin/kustomize"
  CONFIG_TMP_DIR=$(mktemp -d)
  cp -r "${ROOT_DIR}/config"/* "${CONFIG_TMP_DIR}"
  (
      cd "${CONFIG_TMP_DIR}/default"
      "${KUSTOMIZE}" edit add patch manager_image_pull_secret.yaml
      cd "${CONFIG_TMP_DIR}/manager"
      "${KUSTOMIZE}" edit set image "controller=${CONTROLLER_IMG}"
      "${KUSTOMIZE}" edit add patch env_override.yaml
      "${KUSTOMIZE}" edit add configmap controller-manager-env "--from-literal=POSTGRES_IMAGE_NAME=${POSTGRES_IMG}"
  )
  "${KUSTOMIZE}" build "${CONFIG_TMP_DIR}/default" > "${ROOT_DIR}/tests/upgrade/fixtures/current-manifest.yaml"

  # Wait for the v1alpha1 operator (0.7.0) to be up and running
  kubectl wait --for=condition=Available --timeout=2m \
    -n postgresql-operator-system deployments \
    postgresql-operator-controller-manager

  # Run the upgrade tests
  mkdir -p "${ROOT_DIR}/tests/upgrade/out"
  # Unset DEBUG to prevent k8s from spamming messages
  unset DEBUG
  ginkgo --nodes=1 --slowSpecThreshold=300 -v "${ROOT_DIR}/tests/upgrade/..."
fi

CONTROLLER_IMG="${CONTROLLER_IMG}" \
  POSTGRES_IMAGE_NAME="${POSTGRES_IMG}" \
  make -C "${ROOT_DIR}" deploy
kubectl wait --for=condition=Available --timeout=2m \
  -n postgresql-operator-system deployments \
  postgresql-operator-controller-manager

# Unset DEBUG to prevent k8s from spamming messages
unset DEBUG

# Build kubectl-cnp and export its path
make build
export PATH=${ROOT_DIR}/bin/:${PATH}

mkdir -p "${ROOT_DIR}/tests/e2e/out"
# Create at most 4 testing nodes. Using -p instead of --nodes
# would create CPUs-1 nodes and saturate the testing server
ginkgo --nodes=4 --slowSpecThreshold=300 -v "${ROOT_DIR}/tests/e2e/..."

mkdir -p "${ROOT_DIR}/tests/sequential/out"
# These e2e tests need to be run sequentially since they may break concurrent test run
ginkgo --nodes=1 --slowSpecThreshold=300 -v "${ROOT_DIR}/tests/sequential/..."

mkdir -p "${ROOT_DIR}/tests/performance/out"
# Performance tests need to run on a single node to avoid concurrency
ginkgo --nodes=1 --slowSpecThreshold=300 -v "${ROOT_DIR}/tests/performance/..."
