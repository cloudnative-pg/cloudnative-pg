#!/usr/bin/env bash
#
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-20.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
#

# Kind-specific image loading orchestration.
set -eEuo pipefail

# This script is sourced by manage.sh. It assumes common helpers are loaded.

# The Kind-specific loader function, executed by manage.sh
function load_operator_image_vendor_specific() {
  local cluster_name=${CLUSTER_NAME}

  # 1. Execute the generic build and push to the local registry
  build_and_load_operator_image_from_sources

  # 2. Perform cluster-specific loading (Kind requires 'kind load' into nodes)
  # Load the primary operator image
  CONTROLLER_IMG="$(print_image)"
  echo "Loading ${CONTROLLER_IMG} into Kind nodes."
  kind load -v 1 docker-image --name "${cluster_name}" "${CONTROLLER_IMG}"
  echo "Loaded ${CONTROLLER_IMG} into Kind nodes."

  # Load the prime image if upgrade testing is enabled
  if [[ "${TEST_UPGRADE_TO_V1}" != "false" ]]; then
    PRIME_CONTROLLER_IMG="${CONTROLLER_IMG}-prime"
    echo "Loading ${PRIME_CONTROLLER_IMG} into Kind nodes."
    kind load -v 1 docker-image --name "${cluster_name}" "${PRIME_CONTROLLER_IMG}"
    echo "Loaded ${PRIME_CONTROLLER_IMG} into Kind nodes."
  fi
}
