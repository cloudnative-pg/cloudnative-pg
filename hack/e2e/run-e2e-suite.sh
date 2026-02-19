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
go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1

# Unset DEBUG to prevent k8s from spamming messages
unset DEBUG

LABEL_FILTERS="${FEATURE_TYPE//,/ || }"
readonly LABEL_FILTERS

echo "E2E tests are running with the following filters: ${LABEL_FILTERS}"

mkdir -p "${ROOT_DIR}/tests/e2e/out"
RC_GINKGO=0
export TEST_SKIP_UPGRADE=true
ginkgo --nodes=4 --timeout 3h --poll-progress-after=1200s --poll-progress-interval=150s \
       ${LABEL_FILTERS:+--label-filter "${LABEL_FILTERS}"} \
       ${GITHUB_ACTIONS:+--github-output} --force-newlines \
       --output-dir "${ROOT_DIR}/tests/e2e/out/" \
       --json-report  "report.json" -v "${ROOT_DIR}/tests/e2e/..." || RC_GINKGO=$?

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
