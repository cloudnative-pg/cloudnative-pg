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

# This file contains the generic logic for loading images into the local cluster.

# Requires: 00-paths.sh, 10-config.sh, 40-utils-registry.sh

# Load helper images using the vendor specific function
function load_helper_images_vendor_specific() {
    local vendor="${1:-}"

    if [[ -z "${vendor}" ]]
    then
        echo "ERROR: missing vendor when loading helper images." >&2
        exit 1
    fi

    # shellcheck disable=SC2154,SC2153
    echo -e "${bright}Loading helper images for tests on cluster ${CLUSTER_NAME}${reset}"

    local cluster_name=${CLUSTER_NAME}

    # Pre-load all the images defined in the HELPER_IMGS variable
    # with the goal to speed up the runs.
    for IMG in "${HELPER_IMGS[@]}"; do
        echo -e "${bright}Pulling '${IMG}'${reset}"
        docker pull "${IMG}"

        echo -e "${bright}Loading '${IMG}' into ${vendor} nodes for ${cluster_name}${reset}"
        "load_image_${vendor}" "${cluster_name}" "${IMG}"
    done

    echo -e "${bright}Done loading helper images on cluster ${cluster_name}${reset}"
}

