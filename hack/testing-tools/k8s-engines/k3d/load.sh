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

# K3D-specific image loading orchestration.
set -eEuo pipefail

# This script is sourced by manage.sh. It assumes common helpers are loaded.

# The K3D-specific loader function, executed by manage.sh
function load_operator_image_vendor_specific() {
  # Execute the generic build and push to the local registry
  build_and_load_operator_image_from_sources

  # NOTE: For k3d, we don't need to use 'k3d image import' for operator images.
  # The docker-build target uses 'docker buildx bake --push' which pushes
  # directly to the local registry (registry.dev:5000). K3d nodes are
  # configured via registries.yaml to pull from this registry, so the images
  # will be pulled automatically when the operator is deployed.
  #
  # This is different from helper images (like FluentD) which are explicitly
  # loaded using 'k3d image import' because they're not built with buildx.
}
