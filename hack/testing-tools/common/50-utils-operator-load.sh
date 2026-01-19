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

# The primary function executed to build the images.
function build_and_load_operator_image_from_sources() {
  # NOTE: This function only builds and pushes to the local registry.
  # Cluster-specific loading (e.g., Kind's 'kind load') must be done separately.

  ensure_registry
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

function deploy_operator_from_sources() {
    echo "Deploying operator manifests from current worktree..."

    # Attempt to delete the namespace first (ignore errors if it doesn't exist)
    ${K8S_CLI} delete ns cnpg-system 2> /dev/null || true

    # Run the make target from the project root directory
    make -C "${ROOT_DIR}" deploy "CONTROLLER_IMG=${CONTROLLER_IMG}"

    echo "Operator deployment initiated."
}
