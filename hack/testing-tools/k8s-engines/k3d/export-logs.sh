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

# K3D-specific log export logic.
set -eEuo pipefail

# Load common library for access to CLUSTER_NAME and LOG_DIR
DIR="$(dirname "${BASH_SOURCE[0]}")"
COMMON_DIR="${DIR}/../../common"
source "${COMMON_DIR}/00-paths.sh"

# --- HELPER FUNCTION ---

# export_logs_k3d: Exports the logs from all k3d nodes.
function export_logs_k3d() {
  local cluster_name=$1
  local NODES_LIST=()
  while IFS= read -r line; do
    NODES_LIST+=("$line")
  done < <(k3d node list | awk "/${cluster_name}/{print \$1}")
  for i in "${NODES_LIST[@]}"; do
    mkdir -p "${LOG_DIR}/${i}"
    docker cp -L "${i}:/var/log/." "${LOG_DIR}/${i}"
  done
}

# --- MAIN EXECUTION ---

main() {
  # shellcheck disable=SC2153
  export_logs_k3d "${CLUSTER_NAME}"
}

main
