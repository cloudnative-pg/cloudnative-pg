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

# shellcheck disable=SC1090,SC1091

# standard bash error handling
set -eEuo pipefail

if [ "${DEBUG-}" = true ]; then
  set -x
fi

# Define paths for the modular testing framework
ROOT_DIR=$(cd "$(dirname "$0")/../"; pwd)
TESTING_TOOLS_DIR="${ROOT_DIR}/hack/testing-tools"
CLUSTER_MGR_SCRIPT="${TESTING_TOOLS_DIR}/k8s-engines/manage.sh"
COMMON_DIR="${TESTING_TOOLS_DIR}/common"

# Declare the Cluster Engine variable
export CLUSTER_ENGINE="${CLUSTER_ENGINE:-kind}"

source "${TESTING_TOOLS_DIR}/k8s-engines/${CLUSTER_ENGINE}/settings.sh"

# Capture if user explicitly set CLUSTER_NAME before loading config
CLUSTER_NAME_USER_SET="${CLUSTER_NAME:-}"

# Load common configuration: paths, versions, registry settings
source "${COMMON_DIR}/00-paths.sh"
source "${COMMON_DIR}/10-config.sh"
source "${COMMON_DIR}/40-utils-registry.sh"

NODES=${NODES:-3}

usage() {
  cat >&2 <<EOF
Usage: $0 [-k <version>] [-n <nodes>] <command>

Commands:
    create                Create the test cluster and a local registry
    load                  Build and load the operator image in the local registry
    deploy                Deploy the operator manifests in the cluster
    load-helper-images    Load the catalog of helper images in the local registry
    print-image           Print the CONTROLLER_IMG name to be used inside the cluster
    export-logs           Export the logs from the cluster
    teardown              Tear down the cluster
    destroy               alias of teardown
    pyroscope             Deploy Pyroscope and enable pprof for the operator

Options:
    -e|--engine
        <CLUSTER_ENGINE>  Specify the kubernetes cluster's engine to run. Current
                          available options are 'kind' and 'k3d'. Default: 'kind'.
                          Env: CLUSTER_ENGINE

    -k|--k8s-version
        <K8S_VERSION>     Use the specified kubernetes full version number
                          (e.g., v1.35.0). Env: K8S_VERSION

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
  # Parse command-line options (-k, -n, etc.) using getopt
  if ! getopt -T > /dev/null; then
    parsed_opts=$(getopt -o e:k:n:r -l "engine:,k8s-version:,nodes:,registry" -- "$@") || usage
  else
    parsed_opts=$(getopt e:k:n:r "$@") || usage
  fi
  eval "set -- $parsed_opts"

  for o; do
    case "${o}" in
      -e | --engine)
        shift
        export CLUSTER_ENGINE="${1}"
        shift
        ;;
      -k | --k8s-version)
        shift
        # Export K8S_VERSION for the dispatcher
        export K8S_VERSION="v${1#v}"
        shift
        if ! [[ $K8S_VERSION =~ ^v1\.[0-9]+\.[0-9]+$ ]]; then
          echo "ERROR: $K8S_VERSION is not a valid k8s version!" >&2
          echo >&2
          usage
        fi
        ;;
      -n | --nodes)
        shift
        # Export NODES for the dispatcher
        export NODES="${1}"
        shift
        if ! [[ $NODES =~ ^[1-9][0-9]*$ ]]; then
          echo "ERROR: $NODES is not a positive integer!" >&2
          echo >&2
          usage
        fi
        ;;
      -r | --registry)
        shift
        # no-op, kept for compatibility
        ;;
      --)
        shift
        break
        ;;
    esac
  done

  # Recalculate CLUSTER_NAME only if user didn't explicitly set it before running this script
  # User-specified names are preserved, auto-calculated names use the updated K8S_VERSION
  if [ -z "${CLUSTER_NAME_USER_SET}" ]; then
    unset CLUSTER_NAME
  fi

  # Ensure command exists
  if [ "$#" -eq 0 ]; then
    echo "ERROR: you must specify a command" >&2
    usage
  fi

  # --- COMMAND EXECUTION LOOP ---
  # Process all commands in sequence as provided on command line
  while [ "$#" -gt 0 ]; do
    command=$1
    shift

    # Alias handling for backward compatibility
    if [ "$command" == "destroy" ]; then
      command="teardown"
      echo "NOTE: Command 'destroy' aliased to 'teardown'."
    fi

    # Invoke the command through the dispatcher
    case "$command" in
    create | load | load-helper-images | deploy | print-image | export-logs | teardown | pyroscope)
      "${CLUSTER_MGR_SCRIPT}" "${command}"
      ;;
    *)
      echo "ERROR: unknown command ${command}" >&2
      echo >&2
      usage
      ;;
    esac
  done
}

main "$@"
