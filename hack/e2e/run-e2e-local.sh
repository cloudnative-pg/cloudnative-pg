#!/usr/bin/env bash

##
## Copyright The CloudNativePG Contributors
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

# standard bash error handling
set -eEuo pipefail

ROOT_DIR=$(realpath "$(dirname "$0")/../../")
FEATURE_TYPE=${FEATURE_TYPE:-""}
readonly ROOT_DIR

if [ "${DEBUG-}" = true ]; then
  set -x
fi

function get_default_storage_class() {
  kubectl get storageclass -o json | jq  -r 'first(.items[] | select (.metadata.annotations["storageclass.kubernetes.io/is-default-class"] == "true") | .metadata.name)'
}

function get_postgres_image() {
  grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | cut -f 2 -d \"
}

export E2E_DEFAULT_STORAGE_CLASS=${E2E_DEFAULT_STORAGE_CLASS:-$(get_default_storage_class)}
export POSTGRES_IMG=${POSTGRES_IMG:-$(get_postgres_image)}

# Unset DEBUG to prevent k8s from spamming messages
unset DEBUG

LABEL_FILTERS=${FEATURE_TYPE//,/ ||}
readonly LABEL_FILTERS

echo "E2E tests are running with the following filters: ${LABEL_FILTERS}"

mkdir -p "${ROOT_DIR}/tests/e2e/out"
RC_GINKGO=0
export TEST_SKIP_UPGRADE=true
ginkgo --nodes=4 --timeout 3h --poll-progress-after=1200s --poll-progress-interval=150s \
       ${LABEL_FILTERS:+--label-filter "${LABEL_FILTERS}"} \
       --output-dir "${ROOT_DIR}/tests/e2e/out/" \
       --json-report  "report.json" -v "${ROOT_DIR}/tests/e2e/..." || RC_GINKGO=$?

# Report if there are any tests that failed and did NOT have an "ignore-fails" label
RC=0
jq -e -c -f "${ROOT_DIR}/hack/e2e/test-report.jq" "${ROOT_DIR}/tests/e2e/out/report.json" || RC=$?

# The exit code reported depends on the two `jq` filter calls. In case we have
# FAIL in the Ginkgo, but the `jq` succeeds because the failures are ignorable,
# we should add some explanation
set +x
if [[ $RC == 0 ]]; then
  if [[ $RC_GINKGO != 0 ]]; then
    printf "\033[0;32m%s\n" "SUCCESS. All the failures in Ginkgo are labelled 'ignore-fails'."
    echo
  fi
fi
