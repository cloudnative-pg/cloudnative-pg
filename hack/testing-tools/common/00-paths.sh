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

# Standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

# --- PATHS AND ARCHITECTURE ---
ROOT_DIR=$(realpath "$(dirname "${BASH_SOURCE[0]}")/../../../")
HACK_DIR="${ROOT_DIR}/hack"
TESTING_TOOLS_DIR="${HACK_DIR}/testing-tools"
E2E_DIR="${HACK_DIR}/e2e"

# Add go binaries to PATH if go is installed
if command -v go &> /dev/null; then
  GO_BIN="$(go env GOPATH)/bin"
  export PATH="${GO_BIN}:${PATH}"
else
  echo "WARNING: go binary not found in PATH. Some tools may not be available." >&2
fi

export ROOT_DIR HACK_DIR TESTING_TOOLS_DIR E2E_DIR

TEMP_DIR="$(mktemp -d)"
LOG_DIR=${LOG_DIR:-$ROOT_DIR/_logs/}
trap 'rm -fr ${TEMP_DIR}' EXIT

# Architecture detection
ARCH=$(uname -m)
case $ARCH in
  x86_64) ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac
DOCKER_DEFAULT_PLATFORM=${DOCKER_DEFAULT_PLATFORM:-}
if [ "${DOCKER_DEFAULT_PLATFORM}" = "" ]; then
  DOCKER_DEFAULT_PLATFORM="linux/${ARCH}"
fi
export DOCKER_DEFAULT_PLATFORM ARCH

# --- TERMINAL COLOR / FORMATTING DEFINITIONS ---
bright='\033[1m'
reset='\033[0m'

# Check if stdout is a terminal; if not, clear the codes to prevent output pollution.
if [ ! -t 1 ]; then
  bright=''
  reset=''
fi
export bright reset
# -----------------------------------------------

# Determine K8s CLI tool (kubectl or oc)
export K8S_CLI="kubectl"
if [ "${CLUSTER_ENGINE:-}" == "ocp" ]; then
    export K8S_CLI="oc"
fi

# --- TOOL VALIDATION ---

# validate_required_tools: Checks that all required tools are installed
# Usage: validate_required_tools tool1 tool2 tool3...
function validate_required_tools() {
  local missing_tools=()
  for tool in "$@"; do
    if ! command -v "$tool" &>/dev/null; then
      missing_tools+=("$tool")
    fi
  done

  if [ ${#missing_tools[@]} -gt 0 ]; then
    echo "ERROR: The following required tools are not installed:" >&2
    for tool in "${missing_tools[@]}"; do
      echo "  - $tool" >&2
    done
    echo "" >&2
    echo "Please install the missing tools and try again." >&2
    return 1
  fi
  return 0
}
