#!/usr/bin/env bash
#
# Copyright © contributors to CloudNativePG, established as
# CloudNativePG a Series of LF Projects, LLC.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0
#

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

# Define path to the new modular framework root
ROOT_DIR=$(cd "$(dirname "$0")/../"; pwd)
TESTING_TOOLS_DIR="${ROOT_DIR}/hack/testing-tools"
CLUSTER_MGR_SCRIPT="${TESTING_TOOLS_DIR}/k8s-engines/manage.sh"
COMMON_DIR="${TESTING_TOOLS_DIR}/common"

# Source necessary common files to define paths, constants, and utility functions
#source "${COMMON_DIR}/00-paths.sh"
source "${COMMON_DIR}/10-config.sh"

NODES=${NODES:-3}

usage() {
  cat >&2 <<EOF
Usage: $0 [-k <version>] [-r] <command>

Commands:
    create                Create the test cluster and a local registry
    load                  Build and load the operator image in the local registry
    deploy                Deploy the operator manifests in the cluster
    load-helper-images    Load the catalog of helper images in the local registry
    print-image           Print the CONTROLLER_IMG name to be used inside
                          the cluster
    export-logs           Export the logs from the cluster
    teardown              Tear down the cluster
    destroy               alias of teardown
    pyroscope             Deploy Pyroscope and enable pprof for the operator

Options:
    -k|--k8s-version
        <K8S_VERSION>     Use the specified kubernetes full version number
                          (e.g., v1.27.0). Env: K8S_VERSION

    -n|--nodes
        <NODES>           Create a cluster with the required number of nodes.
                          Used only during "create" command. Default: 3
                          Env: NODES

To use long options you need to have GNU enhanced getopt available, otherwise
you can only use the short version of the options.
EOF
  exit 1
}


main() {
  # --- ARGUMENT PARSING ---
  # Use getopt to capture options like -k and -n, just like the original script.
  if ! getopt -T > /dev/null; then
    parsed_opts=$(getopt -o k:n: -l "k8s-version:,nodes:" -- "$@") || usage
  else
    parsed_opts=$(getopt k:n: "$@") || usage
  fi
  eval "set -- $parsed_opts"

  for o; do
    case "${o}" in
      -k | --k8s-version)
        shift
        # Export K8S_VERSION for the dispatcher
        export K8S_VERSION="v${1#v}"
        shift
        ;;
      -n | --nodes)
        shift
        # Export NODES for the dispatcher
        export NODES="${1}"
        shift
        ;;
      --)
        shift
        break
        ;;
    esac
  done

  # Set default K8S_VERSION if not provided by flag or environment
  if [ -z "${K8S_VERSION}" ]; then
    export K8S_VERSION=${KIND_NODE_DEFAULT_VERSION}
  fi

  # NOTE: CLUSTER_NAME will be automatically calculated inside manage.sh
  # when it sources 10-config.sh, which is cleaner than calculating it here.
  # Unset CLUSTER_NAME so it gets recalculated with the updated K8S_VERSION.
  unset CLUSTER_NAME

  # Ensure command exists
  if [ "$#" -eq 0 ]; then
    echo "ERROR: you must specify a command" >&2
    usage
  fi

  # --- ALIAS HANDLING FOR BACKWARD COMPATIBILITY ---
  # If the first argument is 'destroy', change it to 'teardown'.
  if [ "$1" == "destroy" ]; then
    set -- "teardown" "${@:2}"
    echo "NOTE: Command 'destroy' aliased to 'teardown'."
  fi

  # --- DELEGATION TO MODULAR SYSTEM ---
  # Pass all remaining arguments (the command and any subsequent args) to the dispatcher.
  "${CLUSTER_MGR_SCRIPT}" "$@"
}

main "$@"
