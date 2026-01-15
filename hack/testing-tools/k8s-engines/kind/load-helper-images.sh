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

# Kind-specific helper image loading logic.
set -eEuo pipefail

# Load common modules (needed for generic registry push, CLUSTER_NAME, and HELPER_IMGS)
DIR="$(dirname "${BASH_SOURCE[0]}")"
COMMON_DIR="${DIR}/../../common"
source "${COMMON_DIR}/00-paths.sh"
source "${COMMON_DIR}/10-config.sh"
source "${COMMON_DIR}/40-utils-registry.sh" # Contains push_helper_images_to_registry

# --- KIND SPECIFIC HELPER ---

# load_image_kind: Executes the necessary 'kind load' command.
function load_image_kind() {
  local cluster_name=$1
  local image=$2
  kind load -v 1 docker-image --name "${cluster_name}" "${image}"
}

# --- MAIN EXECUTION FUNCTION ---

# This function is executed by the manage.sh dispatcher.
function load_helper_images_vendor_specific() {
    echo -e "${bright}Loading helper images for tests on cluster ${CLUSTER_NAME}${reset}"

    local cluster_name=${CLUSTER_NAME}

    # Pre-load all the images defined in the HELPER_IMGS variable
    # with the goal to speed up the runs.
    for IMG in "${HELPER_IMGS[@]}"; do
        echo -e "${bright}Pulling '${IMG}'${reset}"
        docker pull "${IMG}"

        echo -e "${bright}Loading '${IMG}' into Kind nodes for ${cluster_name}${reset}"
        load_image_kind "${cluster_name}" "${IMG}"
    done

    echo -e "${bright}Done loading helper images on cluster ${cluster_name}${reset}"
}
