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

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
FEATURE_TYPE=${FEATURE_TYPE:-""}
readonly ROOT_DIR

if [ "${DEBUG-}" = true ]; then
  set -x
fi

function notinpath() {
    case "$PATH" in
        *:$1:* | *:$1 | $1:*)
            return 1
            ;;
        *)
            return 0
            ;;
    esac
}

export POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export PGBOUNCER_IMG=${PGBOUNCER_IMG:-$(grep 'DefaultPgbouncerImage.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \")}
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}

# Ensure GOBIN is in path, we'll use this to install and execute ginkgo
go_bin="$(go env GOPATH)/bin"
if notinpath "${go_bin}"; then
  export PATH="${go_bin}:${PATH}"
fi

# renovate: datasource=github-releases depName=onsi/ginkgo
go install github.com/onsi/ginkgo/v2/ginkgo@v2.32.0

# Unset DEBUG to prevent k8s from spamming messages
unset DEBUG

LABEL_FILTERS="${FEATURE_TYPE//,/ || }"
readonly LABEL_FILTERS

echo "E2E tests are running with the following filters: ${LABEL_FILTERS}"

mkdir -p "${ROOT_DIR}/tests/e2e/out"
RC_GINKGO=0
export TEST_SKIP_UPGRADE=true
cd "${ROOT_DIR}/tests"

# E2E_GINKGO_NODES overrides everything below when set, for pinning
# parallelism by hand (debugging a flake, or a runner/vendor the heuristic
# below doesn't fit).
if [ -n "${E2E_GINKGO_NODES:-}" ]; then
  GINKGO_NODES="${E2E_GINKGO_NODES}"
  echo "E2E_GINKGO_NODES set; running ginkgo with ${GINKGO_NODES} node(s)"
else
  # kind/k3d run their control plane (apiserver/etcd/kubelet/containerd) on
  # the same runner as the ginkgo workers, so parallelism must be sized to
  # the runner to leave it headroom. A bare/unrecognized TEST_CLOUD_VENDOR
  # (e.g. a local run outside run-e2e-local.sh) falls into this case too,
  # since that always means a local kind/k3d cluster. The remote cloud
  # vendors below only talk to a cluster over the network, so they gain
  # nothing from capping parallelism to the runner's own core count and
  # instead just need enough workers to fit the suite in the CI timeout.
  case "${TEST_CLOUD_VENDOR:-}" in
    eks|gke|aks|ocp)
      GINKGO_NODES=4
      echo "Running on ${TEST_CLOUD_VENDOR}; running ginkgo with ${GINKGO_NODES} node(s)"
      ;;
    *)
      # Reserve ~25% of the available cores for the control plane and system
      # pods, and use the rest as ginkgo workers.
      AVAILABLE_CORES=$(nproc)
      RESERVED_CORES=$(( (AVAILABLE_CORES + 3) / 4 ))
      GINKGO_NODES=$(( AVAILABLE_CORES - RESERVED_CORES ))
      if [ "${GINKGO_NODES}" -lt 1 ]; then
        GINKGO_NODES=1
      fi
      echo "Detected ${AVAILABLE_CORES} CPU(s); running ginkgo with ${GINKGO_NODES} node(s)"
      ;;
  esac
fi

ginkgo --nodes="${GINKGO_NODES}" --timeout 3h --poll-progress-after=1200s --poll-progress-interval=150s \
      ${LABEL_FILTERS:+--label-filter "${LABEL_FILTERS}"} \
      ${GITHUB_ACTIONS:+--github-output} --force-newlines \
      --output-dir "${ROOT_DIR}/tests/e2e/out/" \
      --json-report  "report.json" -v ./e2e/... || RC_GINKGO=$?

# Report if there are any tests that failed
RC=0
jq -e -c -f "${ROOT_DIR}/hack/e2e/test-report.jq" "${ROOT_DIR}/tests/e2e/out/report.json" || RC=$?

# Handle a known ginkgo race condition: when running in parallel mode,
# ginkgo has a hardcoded 1-second timeout waiting for the parallel server
# to finalize. If the timeout fires, the per-suite JSON report is never
# created, producing an empty merged report that fails the jq check above.
# When ginkgo itself exited 0 (all tests passed) but the report is empty,
# trust ginkgo's exit code.
if [[ $RC -ne 0 && $RC_GINKGO -eq 0 ]]; then
  echo "WARNING: ginkgo exited 0 (all tests passed) but the JSON report check failed."
  echo "This is likely the known ginkgo parallel report finalization race condition."
  echo "Trusting ginkgo's exit code."
  RC=0
fi

set +x
if [[ $RC == 0 ]]; then
  if [[ $RC_GINKGO != 0 ]]; then
    printf "\033[0;32m%s\n" "SUCCESS."
    echo
  fi
fi

exit $RC
