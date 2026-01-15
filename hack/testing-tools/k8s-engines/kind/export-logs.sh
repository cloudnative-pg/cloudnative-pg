#!/usr/bin/env bash
#
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-20.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
#
# shellcheck disable=SC1090,SC1091

# Kind-specific log export logic.
set -eEuo pipefail

# Load common library for access to CLUSTER_NAME and LOG_DIR
DIR="$(dirname "${BASH_SOURCE[0]}")"
COMMON_DIR="${DIR}/../../common"
source "${COMMON_DIR}/00-paths.sh" 

# --- HELPER FUNCTION ---

# export_logs_kind: Executes the Kind log export command.
function export_logs_kind() {
  local cluster_name=$1
  echo "Exporting Kind logs for cluster '${cluster_name}' to directory: ${LOG_DIR}"
  kind export logs "${LOG_DIR}" --name "${cluster_name}"
}

# --- MAIN EXECUTION ---

main() {
  # shellcheck disable=SC2153
  export_logs_kind "${CLUSTER_NAME}"
}

main
