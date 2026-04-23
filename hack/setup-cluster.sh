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

# build_and_load_operator_image_from_sources: builds the operator image via
# 'make docker-build' and pushes it to the local registry.
function build_and_load_operator_image_from_sources() {
  create_builder

  # shellcheck disable=SC2154
  echo -e "${bright}Building operator from current worktree${reset}"

  CONTROLLER_IMG="$(print_image)"

  # shellcheck disable=SC2154
  make -C "${ROOT_DIR}" CONTROLLER_IMG="${CONTROLLER_IMG}" insecure="true" \
    ARCH="${ARCH}" BUILDER_NAME="${builder_name}" docker-build

  echo -e "${bright}Done building and pushing new operator image on local registry.${reset}"

  if [[ "${TEST_UPGRADE_TO_V1}" != "false" ]]; then
    echo -e "${bright}Building a 'prime' operator from current worktree${reset}"

    PRIME_CONTROLLER_IMG="${CONTROLLER_IMG}-prime"
    CURRENT_VERSION=$(make -C "${ROOT_DIR}" -s print-version)
    PRIME_VERSION="${CURRENT_VERSION}-prime"

    make -C "${ROOT_DIR}" CONTROLLER_IMG="${PRIME_CONTROLLER_IMG}" VERSION="${PRIME_VERSION}" insecure="true" \
      ARCH="${ARCH}" BUILDER_NAME="${builder_name}" docker-build

    echo -e "${bright}Done building and pushing 'prime' operator image on local registry.${reset}"
  fi

  docker buildx rm "${builder_name}"
}

# generate_operator_manifest: renders the kustomize manifest for the locally
# built operator image, writing to OPERATOR_MANIFEST_PATH.
function generate_operator_manifest() {
  # shellcheck disable=SC2154
  echo -e "${bright}Generating operator manifest at ${OPERATOR_MANIFEST_PATH}${reset}"
  CONTROLLER_IMG="${CONTROLLER_IMG:-$(print_image)}" \
      POSTGRES_IMAGE_NAME="${POSTGRES_IMG}" \
      PGBOUNCER_IMAGE_NAME="${PGBOUNCER_IMG}" \
      make -C "${ROOT_DIR}" generate-manifest
  echo -e "${bright}Operator manifest generated.${reset}"
}

usage() {
  cat >&2 <<EOF
Usage: $0 [-e <engine>] [-k <version>] [-n <nodes>] [-o <operator>] [-d <method>] <command>

Commands:
    create                Create the test cluster and a local registry
    load                  Build and load the operator image in the local registry
    generate-manifest     Generate the operator manifest from the local worktree
    deploy                Generate the manifest (if OPERATOR=local) and deploy the operator
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

    -o|--operator
      <OPERATOR>          Controls which version of the operator is deployed.
                          Use 'local' (default) to build and deploy from the
                          local worktree, an exact version such as '1.28.1' to
                          deploy that published release, or a branch name such
                          as 'main' or 'release-1.28' to deploy the latest
                          snapshot for that branch.
                          Env: OPERATOR

    -d|--deployment-method
      <METHOD>            Deployment method for the operator. Use 'manifest'
                          (default) to deploy via a kustomize-generated
                          manifest, or 'helm' to deploy via the official
                          Helm chart. Only 'manifest' is supported when
                          OPERATOR is not 'local'.
                          Env: CNPG_DEPLOYMENT_METHOD

To use long options you need to have GNU enhanced getopt available, otherwise
you can only use the short version of the options.
EOF
  exit 1
}


main() {
  # --- ARGUMENT PARSING ---
  # Parse command-line options (-k, -n, etc.) using getopt
  if ! getopt -T > /dev/null; then
    parsed_opts=$(getopt -o e:k:n:o:d:r -l "engine:,k8s-version:,nodes:,operator:,deployment-method:,registry" -- "$@") || usage
  else
    parsed_opts=$(getopt e:k:n:o:d:r "$@") || usage
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
      -o | --operator)
        shift
        export OPERATOR="${1}"
        shift
        ;;
      -d | --deployment-method)
        shift
        export CNPG_DEPLOYMENT_METHOD="${1}"
        shift
        if [[ "${CNPG_DEPLOYMENT_METHOD}" != "manifest" ]] && [[ "${CNPG_DEPLOYMENT_METHOD}" != "helm" ]]; then
          echo "ERROR: '${CNPG_DEPLOYMENT_METHOD}' is not a valid deployment method. Use 'manifest' or 'helm'." >&2
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
    load)
      if [[ "${OPERATOR}" != "local" ]]; then
        echo "Skipping image build: OPERATOR=${OPERATOR}"
      else
        build_and_load_operator_image_from_sources
      fi
      ;;
    generate-manifest)
      generate_operator_manifest
      ;;
    deploy)
      if [[ "${OPERATOR}" == "local" ]] && [[ "${CNPG_DEPLOYMENT_METHOD}" != "helm" ]]; then
        generate_operator_manifest
      fi
      "${CLUSTER_MGR_SCRIPT}" "${command}"
      ;;
    create | load-helper-images | print-image | export-logs | teardown | pyroscope)
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
