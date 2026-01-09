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

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
    set -x
fi

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
CONTROLLER_IMG=${CONTROLLER_IMG:-$("${ROOT_DIR}/hack/setup-cluster.sh" print-image)}
CONTROLLER_IMG_DIGEST=${CONTROLLER_IMG_DIGEST:-""}
CONTROLLER_IMG_PRIME_DIGEST=${CONTROLLER_IMG_PRIME_DIGEST:-""}
TEST_UPGRADE_TO_V1=${TEST_UPGRADE_TO_V1:-true}
POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
PGBOUNCER_IMG=${PGBOUNCER_IMG:-$(grep 'DefaultPgbouncerImage.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}

# Override pgbouncer image repository if PGBOUNCER_IMG_REPOSITORY is set
if [ -n "${PGBOUNCER_IMG_REPOSITORY:-}" ]; then
  PGBOUNCER_VERSION=$(echo "${PGBOUNCER_IMG}" | cut -d: -f2)
  PGBOUNCER_IMG="${PGBOUNCER_IMG_REPOSITORY}:${PGBOUNCER_VERSION}"
fi

# variable need export otherwise be invisible in e2e test case
export DOCKER_SERVER=${DOCKER_SERVER:-${REGISTRY:-}}
export DOCKER_USERNAME=${DOCKER_USERNAME:-${REGISTRY_USER:-}}
export DOCKER_PASSWORD=${DOCKER_PASSWORD:-${REGISTRY_PASSWORD:-}}

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

ensure_image_pull_secret() {
  if [ -n "${DOCKER_SERVER-}" ] && [ -n "${DOCKER_USERNAME-}" ] && [ -n "${DOCKER_PASSWORD-}" ]; then
    if ! kubectl get secret cnpg-pull-secret -n cnpg-system >/dev/null 2>&1; then
      kubectl create secret docker-registry \
        -n cnpg-system \
        cnpg-pull-secret \
        --docker-server="${DOCKER_SERVER}" \
        --docker-username="${DOCKER_USERNAME}" \
        --docker-password="${DOCKER_PASSWORD}"
    fi
  fi
}

# Process the e2e templates
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}
export AZURE_STORAGE_ACCOUNT=${AZURE_STORAGE_ACCOUNT:-''}

go_bin="$(go env GOPATH)/bin"
if notinpath "${go_bin}"; then
  export PATH="${go_bin}:${PATH}"
fi

# renovate: datasource=github-releases depName=onsi/ginkgo
go install github.com/onsi/ginkgo/v2/ginkgo@v2.27.4


LABEL_FILTERS=""
if [ "${FEATURE_TYPE-}" ]; then
  LABEL_FILTERS="${FEATURE_TYPE//,/ || }"
fi
echo "E2E tests are running with the following filters: ${LABEL_FILTERS}"
# The RC return code will be non-zero iff either the two `jq` calls has a non-zero exit
RC=0
RC_GINKGO1=0
if [[ "${TEST_UPGRADE_TO_V1}" != "false" ]] && [[ "${TEST_CLOUD_VENDOR}" != "ocp" ]]; then
  # Generate a manifest for the operator so we can upgrade to it in the upgrade tests.
  # This manifest uses the default image and tag for the current operator build, and assumes
  # the image has been either:
  #   - built and pushed to nodes or the local registry (by setup-cluster.sh)
  #   - built by the `buildx` step in continuous delivery and pushed to the test registry
  make CONTROLLER_IMG="${CONTROLLER_IMG}" POSTGRES_IMAGE_NAME="${POSTGRES_IMG}" \
   PGBOUNCER_IMAGE_NAME="${PGBOUNCER_IMG}" \
   CONTROLLER_IMG_DIGEST="${CONTROLLER_IMG_DIGEST}" \
   OPERATOR_MANIFEST_PATH="${ROOT_DIR}/tests/e2e/fixtures/upgrade/current-manifest.yaml" \
   generate-manifest
  # In order to test the case of upgrading from the current operator
  # to a future one, we build and push an image with a different VERSION
  # to force a different hash for the manager binary.
  # (Otherwise the ONLINE upgrade won't trigger)
  #
  # We build and push the new image in the setup-cluster.sh `load` function, or
  # in the `buildx` phase if we're running in the cloud CI/CD workflow.
  #
  # Here we build a manifest for the new controller, with the `-prime` suffix
  # added to the tag by convention, which assumes the image is in place.
  # This manifest is used to upgrade into in the upgrade_test E2E.
  make CONTROLLER_IMG="${CONTROLLER_IMG}-prime" POSTGRES_IMAGE_NAME="${POSTGRES_IMG}" \
   PGBOUNCER_IMAGE_NAME="${PGBOUNCER_IMG}" \
   CONTROLLER_IMG_DIGEST="${CONTROLLER_IMG_PRIME_DIGEST}" \
   OPERATOR_MANIFEST_PATH="${ROOT_DIR}/tests/e2e/fixtures/upgrade/current-manifest-prime.yaml" \
   generate-manifest

  # Run the upgrade tests
  mkdir -p "${ROOT_DIR}/tests/e2e/out"
  # Unset DEBUG to prevent k8s from spamming messages
  unset DEBUG
  unset TEST_SKIP_UPGRADE
  ginkgo --nodes=1 --timeout 90m --poll-progress-after=1200s --poll-progress-interval=150s --label-filter "${LABEL_FILTERS}" \
   --github-output --force-newlines \
   --focus-file "${ROOT_DIR}/tests/e2e/upgrade_test.go" --output-dir "${ROOT_DIR}/tests/e2e/out" \
   --json-report  "upgrade_report.json" -v "${ROOT_DIR}/tests/e2e/..." || RC_GINKGO1=$?

  # Report if there are any tests that failed
  jq -e -c -f "${ROOT_DIR}/hack/e2e/test-report.jq" "${ROOT_DIR}/tests/e2e/out/upgrade_report.json" || RC=$?
fi

if [[ "${TEST_CLOUD_VENDOR}" != "ocp" ]]; then
  # Getting the operator images need a pull secret
  kubectl delete namespace cnpg-system || :
  kubectl create namespace cnpg-system
  ensure_image_pull_secret

  CONTROLLER_IMG="${CONTROLLER_IMG}" \
    POSTGRES_IMAGE_NAME="${POSTGRES_IMG}" \
    PGBOUNCER_IMAGE_NAME="${PGBOUNCER_IMG}" \
    make -C "${ROOT_DIR}" deploy
  kubectl wait --for=condition=Available --timeout=2m \
    -n cnpg-system deployments \
    cnpg-controller-manager
fi

# Unset DEBUG to prevent k8s from spamming messages
unset DEBUG

# Build kubectl-cnpg and export its path
make build-plugin
export PATH=${ROOT_DIR}/bin/:${PATH}

mkdir -p "${ROOT_DIR}/tests/e2e/out"

# Create at most 4 testing nodes. Using -p instead of --nodes
# would create CPUs-1 nodes and saturate the testing server
RC_GINKGO2=0
export TEST_SKIP_UPGRADE=true
ginkgo --nodes=4 --timeout 3h --poll-progress-after=1200s --poll-progress-interval=150s \
       ${LABEL_FILTERS:+--label-filter "${LABEL_FILTERS}"} \
       --github-output --force-newlines \
       --output-dir "${ROOT_DIR}/tests/e2e/out/" \
       --json-report  "report.json" -v "${ROOT_DIR}/tests/e2e/..." || RC_GINKGO2=$?

# Report if there are any tests that failed
jq -e -c -f "${ROOT_DIR}/hack/e2e/test-report.jq" "${ROOT_DIR}/tests/e2e/out/report.json" || RC=$?

set +x
if [[ $RC == 0 ]]; then
  if [[ $RC_GINKGO1 != 0 || $RC_GINKGO2 != 0 ]]; then
    printf "\033[0;32m%s\n" "SUCCESS."
    echo
  fi
fi

exit $RC
