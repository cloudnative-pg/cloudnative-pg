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

set -eEuo pipefail

# Load common modules needed for dispatch logic and the fallback function
DIR="$(dirname "${BASH_SOURCE[0]}")"
COMMON_DIR="${DIR}/../common"

# Source necessary common files to define paths, constants, and utility functions
# shellcheck disable=SC1091
# shellcheck disable=SC1090
source "${COMMON_DIR}/00-paths.sh"
source "${COMMON_DIR}/10-config.sh"
source "${COMMON_DIR}/40-utils-registry.sh"
source "${COMMON_DIR}/50-utils-operator-load.sh"

ACTION="${1:-}"
# VENDOR determines the target directory and defaults to 'kind'
VENDOR="${CLUSTER_ENGINE:-kind}"

if [ -z "$ACTION" ]; then
    echo "Usage: $0 <create|load-from-sources|deploy-from-sources|load-helper-images|print-image|export-logs|teardown|pyroscope|env>"
    exit 1
fi

# --- Action Aliases for Backward Compatibility ---
case "$ACTION" in
    load) ACTION="load-from-sources" ;;
    deploy) ACTION="deploy-from-sources" ;;
esac

VENDOR_DIR="${DIR}/${VENDOR}"

# --- Cluster Lifecycle Operations Dispatch ---
case "$ACTION" in
    create)
        SETUP_SCRIPT="${VENDOR_DIR}/setup.sh"

        if [ -f "${SETUP_SCRIPT}" ]; then
            "${SETUP_SCRIPT}"
        else
            echo "ERROR: Setup script not found for vendor '$VENDOR' at: ${SETUP_SCRIPT}" >&2
            exit 1
        fi
        ;;

    teardown)
        TEARDOWN_SCRIPT="${VENDOR_DIR}/teardown.sh"

        if [ -f "${TEARDOWN_SCRIPT}" ]; then
            "${TEARDOWN_SCRIPT}"
        else
            echo "WARNING: Teardown script not found for vendor '$VENDOR' at: ${TEARDOWN_SCRIPT}. Skipping." >&2
        fi
        ;;

    load-from-sources)
        LOAD_VENDOR_SCRIPT="${VENDOR_DIR}/load.sh"

        if [ -f "${LOAD_VENDOR_SCRIPT}" ]; then
            source "${LOAD_VENDOR_SCRIPT}"
            load_operator_image_vendor_specific
        else
            build_and_load_operator_image_from_sources
        fi
        ;;

    deploy-from-sources)
        # Ensure CONTROLLER_IMG is defined
        CONTROLLER_IMG=${CONTROLLER_IMG:-$(print_image)}

        source "${COMMON_DIR}/20-utils-k8s.sh"
        deploy_operator_from_sources
        ;;

    load-helper-images)
        LOAD_HELPER_SCRIPT="${VENDOR_DIR}/load-helper-images.sh"

        if [ -f "${LOAD_HELPER_SCRIPT}" ]; then
            source "${LOAD_HELPER_SCRIPT}"
            load_helper_images_vendor_specific
        else
            echo "No implementation of 'load-helper-images' for ${VENDOR}"
        fi
        ;;

    print-image)
        print_image
        ;;

    export-logs)
        EXPORT_SCRIPT="${VENDOR_DIR}/export-logs.sh"

        if [ -f "${EXPORT_SCRIPT}" ]; then
            "${EXPORT_SCRIPT}"
        else
            echo "WARNING: Log export script not found for vendor '$VENDOR'. Skipping." >&2
        fi
        ;;

    pyroscope)
        source "${COMMON_DIR}/20-utils-k8s.sh"
        deploy_pyroscope
        echo ">>> Done deploying Pyroscope."
        ;;

    env)
        echo ""
        echo "> FRAMEWORK ENVIRONMENT VARIABLES (Current Session Defaults) <"
        echo "--------------------------------------------------------------"

        # --- CORE FRAMEWORK CONTEXT ---
        echo "--- CORE FRAMEWORK CONTEXT ---"
        echo "ROOT_DIR:                   ${ROOT_DIR}"
        echo "HACK_DIR:                   ${HACK_DIR}"
        echo "TESTING_TOOLS_DIR:          ${TESTING_TOOLS_DIR}"
        echo "K8S_CLI:                    ${K8S_CLI}"
        echo "ARCH:                       ${ARCH}"
        echo "DOCKER_DEFAULT_PLATFORM:    ${DOCKER_DEFAULT_PLATFORM}"

        # --- CLUSTER & KIND CONFIGURATION ---
        echo ""
        echo "--- CLUSTER & KIND CONFIGURATION ---"
        echo "CLUSTER_ENGINE:             ${CLUSTER_ENGINE:-<not explicitly set>}"
        echo "K8S_VERSION:                ${K8S_VERSION}"
        echo "CLUSTER_NAME:               ${CLUSTER_NAME:-<not explicitly set>}"
        echo "NODES:                      ${NODES:-<not explicitly set>}"
        echo "ENABLE_APISERVER_AUDIT:     ${ENABLE_APISERVER_AUDIT:-false}"
        echo "ENABLE_FLUENTD:             ${ENABLE_FLUENTD:-false}"

        # --- IMAGE & BUILD ARTIFACTS ---
        echo ""
        echo "--- IMAGE & BUILD ARTIFACTS ---"
        echo "CONTROLLER_IMG (default):   $(print_image)"
        echo "POSTGRES_IMG:               ${POSTGRES_IMG}"
        echo "E2E_PRE_ROLLING_UPDATE_IMG: ${E2E_PRE_ROLLING_UPDATE_IMG}"
        echo "MINIO_IMG:                  ${MINIO_IMG}"
        echo "HELPER_IMGS count:          ${#HELPER_IMGS[@]}"
        echo "TEST_UPGRADE_TO_V1:         ${TEST_UPGRADE_TO_V1}"

        echo "---------------------------------------------------------------------"
        ;;

    *)
        echo "ERROR: Unknown command: $ACTION" >&2
        exit 1
        ;;
esac
