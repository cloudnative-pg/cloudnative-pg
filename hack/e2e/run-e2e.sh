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
export AZURE_STORAGE_ACCOUNT=${AZURE_STORAGE_ACCOUNT:-''}
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
  go install github.com/onsi/ginkgo/v2/ginkgo
fi

# The RC return code will be non-zero iff either the two `jq` calls has a non-zero exit
# NOTE: the ginkgo calls may have non-zero exits, with E2E tests that fail but could be 'ignore-fail'
RC=0
if [[ "${TEST_UPGRADE_TO_V1}" != "false" ]]; then
  # Generate a manifest for the operator after the api upgrade
  # TODO: this is almost a "make deploy". Refactor.
  make manifests kustomize
  KUSTOMIZE="${ROOT_DIR}/bin/kustomize"
  CONFIG_TMP_DIR=$(mktemp -d)
  cp -r "${ROOT_DIR}/config"/* "${CONFIG_TMP_DIR}"
  (
      cd "${CONFIG_TMP_DIR}/default"
      "${KUSTOMIZE}" edit add patch --path manager_image_pull_secret.yaml
      cd "${CONFIG_TMP_DIR}/manager"
      "${KUSTOMIZE}" edit set image "controller=${CONTROLLER_IMG}"
      "${KUSTOMIZE}" edit add patch --path env_override.yaml
      "${KUSTOMIZE}" edit add configmap controller-manager-env "--from-literal=POSTGRES_IMAGE_NAME=${POSTGRES_IMG}"
  )
  "${KUSTOMIZE}" build "${CONFIG_TMP_DIR}/default" > "${ROOT_DIR}/tests/e2e/fixtures/upgrade/current-manifest.yaml"
  # Run the upgrade tests
  mkdir -p "${ROOT_DIR}/tests/e2e/out"
  # Unset DEBUG to prevent k8s from spamming messages
  unset DEBUG
  RC_GINKGO1=0
  ginkgo --nodes=1 --slow-spec-threshold=5m --label-filter "upgrade" --output-dir "${ROOT_DIR}/tests/e2e/out" --json-report  "upgrade_report.json" -v "${ROOT_DIR}/tests/e2e/..." || RC_GINKGO1=$?

  # Report if there are any tests that failed and did NOT have an "ignore-fails" label
  jq -e -c -f "${ROOT_DIR}/hack/e2e/test-report.jq" "${ROOT_DIR}/tests/e2e/out/upgrade_report.json" || RC=$?
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
RC_GINKGO2=0
ginkgo --nodes=4 --timeout 2h --slow-spec-threshold 5m --label-filter "!(upgrade)" --output-dir "${ROOT_DIR}/tests/e2e/out/" --json-report  "report.json" -v "${ROOT_DIR}/tests/e2e/..." || RC_GINKGO2=$?

# Report if there are any tests that failed and did NOT have an "ignore-fails" label
jq -e -c -f "${ROOT_DIR}/hack/e2e/test-report.jq" "${ROOT_DIR}/tests/e2e/out/report.json" || RC=$?

# The exit code reported depends on the two `jq` filter calls. In case we have
# FAIL in the Ginkgo, but the `jq` succeeds because the failures are ignorable,
# we should add some explanation
set +x
if [[ $RC == 0 ]]; then
  if [[ $RC_GINKGO1 != 0 || $RC_GINKGO2 != 0 ]]; then
    printf "\033[0;32m%s\n" "SUCCESS. All the failures in Ginkgo are labelled 'ignore-fails'."
    echo
  fi
fi

exit $RC
