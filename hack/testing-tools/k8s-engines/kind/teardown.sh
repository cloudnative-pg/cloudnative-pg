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

# shellcheck disable=SC1090,SC1091

# Kind-specific cluster teardown logic.
set -eEuo pipefail

# Load common library to access global vars (registry_name, CLUSTER_NAME)
source "$(dirname "$0")/../../common/00-paths.sh"
source "$(dirname "$0")/../../common/10-config.sh" # For CLUSTER_NAME
source "$(dirname "$0")/../../common/40-utils-registry.sh" # For registry_name

# shellcheck disable=SC2153
echo "Tearing down kind cluster '${CLUSTER_NAME}'."

destroy_kind() {
  local cluster_name=$1
  # shellcheck disable=SC2154
  docker network disconnect "kind" "${registry_name}" &>/dev/null || true
  kind delete cluster --name "${cluster_name}" || true
  docker network rm "kind" &>/dev/null || true
}

destroy_kind "${CLUSTER_NAME}"

echo "Kind cluster '${CLUSTER_NAME}' successfully torn down."
