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

# This file contains the generic logic for building and loading the operator image
# into the local Docker registry.

# Requires: 00-paths.sh, 10-config.sh, 40-utils-registry.sh (for helpers like create_builder, load_image_registry, print_image)

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

# The primary function executed to build the images.
function build_and_load_operator_image_from_sources() {
  # NOTE: This function only builds and pushes to the local registry.
  # Cluster-specific loading (e.g., Kind's 'kind load') must be done separately.

  create_builder # Create the buildx builder instance

  # shellcheck disable=SC2154
  echo -e "${bright}Building operator from current worktree${reset}"

  # Get the target image name (e.g., registry.dev:5000/cloudnative-pg-testing:latest)
  CONTROLLER_IMG="$(print_image)"

  # Build the operator image and push it to the local registry
  # shellcheck disable=SC2154
  make -C "${ROOT_DIR}" CONTROLLER_IMG="${CONTROLLER_IMG}" insecure="true" \
    ARCH="${ARCH}" BUILDER_NAME="${builder_name}" docker-build

  echo -e "${bright}Done building and pushing new operator image on local registry.${reset}"

  if [[ "${TEST_UPGRADE_TO_V1}" != "false" ]]; then
    # Building the 'prime' version for upgrade testing

    echo -e "${bright}Building a 'prime' operator from current worktree${reset}"

    PRIME_CONTROLLER_IMG="${CONTROLLER_IMG}-prime"
    CURRENT_VERSION=$(make -C "${ROOT_DIR}" -s print-version)
    PRIME_VERSION="${CURRENT_VERSION}-prime"

    # Build the prime image with a modified version tag
    make -C "${ROOT_DIR}" CONTROLLER_IMG="${PRIME_CONTROLLER_IMG}" VERSION="${PRIME_VERSION}" insecure="true" \
      ARCH="${ARCH}" BUILDER_NAME="${builder_name}" docker-build

    echo -e "${bright}Done building and pushing 'prime' operator image on local registry.${reset}"
  fi

  docker buildx rm "${builder_name}"
}

function print_operator_image() {
    local image
    image=$(${K8S_CLI} get deployment cnpg-controller-manager -n cnpg-system \
        -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null)
    if [[ -n "${image}" ]]; then
        echo -e "${bright}Operator image: ${image}${reset}"
    fi
}

function deploy_operator_from_sources() {
    echo -e "${bright}Deploying operator manifests from current worktree...${reset}"

    # Attempt to delete the namespace first (ignore errors if it doesn't exist)
    ${K8S_CLI} delete ns cnpg-system 2> /dev/null || true

    # Run the make target from the project root directory
    make -C "${ROOT_DIR}" deploy "CONTROLLER_IMG=${CONTROLLER_IMG}"

    echo -e "${bright}Operator deployment initiated.${reset}"
    print_operator_image
}

function deploy_operator_from_manifest() {
    local operator="${1:?operator is required}"
    local manifest_url

    # Semver version (e.g. 1.28.1) -> published release manifest from the main repo
    # Branch name (e.g. main, release-1.28) -> snapshot manifest from the artifacts repo
    if [[ "${operator}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        manifest_url="https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/v${operator}/releases/cnpg-${operator}.yaml"
    else
        manifest_url="https://raw.githubusercontent.com/cloudnative-pg/artifacts/${operator}/manifests/operator-manifest.yaml"
    fi

    if ! curl --silent --head --fail "${manifest_url}" > /dev/null 2>&1; then
        echo -e "${bright}Error: Manifest not found at ${manifest_url}${reset}" >&2
        echo -e "${bright}Please verify the operator: ${operator}${reset}" >&2
        exit 1
    fi

    echo -e "${bright}Deploying operator from '${operator}'${reset}"
    ${K8S_CLI} delete ns cnpg-system 2>/dev/null || true
    ${K8S_CLI} apply --server-side -f "${manifest_url}" # avoids last-applied-configuration annotation exceeding the 262144 byte limit on large CRDs

    echo -e "${bright}Operator deployment initiated.${reset}"
    print_operator_image
}
