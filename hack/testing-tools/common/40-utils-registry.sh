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

# This file contains functions for managing the local Docker registry and builder.

# --- REGISTRY CONSTANTS ---
registry_volume=registry_dev_data
registry_name=registry.dev
registry_net=registry
registry_port='5000'
builder_name=cnpg-builder

# ensure_registry: Sets up the local Docker registry container and network.
function ensure_registry() {
  echo -e "${bright}Verify local registry${reset}"
  if ! docker volume inspect "${registry_volume}" &>/dev/null; then
    echo "- Create registry volume: ${registry_volume}${reset}"
    docker volume create "${registry_volume}"
  fi
  if ! docker network inspect "${registry_net}" &>/dev/null; then
    echo "- Create registry network: ${registry_net}${reset}"
    docker network create "${registry_net}"
  fi
  if ! docker inspect -f '{{.State.Running}}' "${registry_name}" &>/dev/null; then
    echo "- Start registry: ${registry_name}${reset}"
    docker container run -d --name "${registry_name}" --network "${registry_net}" -v "${registry_volume}:/var/lib/registry" --restart always -p ${registry_port}:5000 registry:2
  fi
  echo -e "${bright}Registry ${registry_name} running${reset}"
}

# create_builder: Sets up a Docker Buildx builder instance.
function create_builder() {
  docker buildx rm "${builder_name}" &>/dev/null || true
  docker buildx create --name "${builder_name}" --driver-opt "network=${registry_net}"
}

# load_image_registry: Pushes a local image to the running development registry.
function load_image_registry() {
  local image=$1
  # DOCKER_DEFAULT_PLATFORM is exported by 00-paths.sh
  local image_local_name=${image/${registry_name}/127.0.0.1}
  echo -e "${bright}Tagging ${image} ${image_local_name}${reset}"
  docker tag "${image}" "${image_local_name}"
  echo -e "${bright}Loading image ${image_local_name} for ${DOCKER_DEFAULT_PLATFORM}${reset}"
  docker push --platform "${DOCKER_DEFAULT_PLATFORM}" -q "${image_local_name}"
}

# print_image: Prints the controller image name used inside the cluster.
function print_image() {
  echo "${registry_name}:${registry_port}/cloudnative-pg-testing:latest"
}
