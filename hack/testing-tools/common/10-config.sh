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

# This file defines global, configurable constants and dependency versions.
# Requires: $ROOT_DIR (from 00-paths.sh)

# --- COMMON IMAGE AND VERSION DEFAULTS ---

# Defines the default cluster name based on the Kubernetes version.
export CLUSTER_NAME=${CLUSTER_NAME:-pg-operator-e2e-${K8S_VERSION//./-}}

# Postgres Image
# Uses awk and tr to ensure clean string extraction, avoiding whitespace/CR issues.
export POSTGRES_IMG=${POSTGRES_IMG:-$(grep 'DefaultImageName.*=' "${ROOT_DIR}/pkg/versions/versions.go" | awk -F '"' '{print $2}' | tr -d '[:space:]')}
export E2E_PRE_ROLLING_UPDATE_IMG=${E2E_PRE_ROLLING_UPDATE_IMG:-${POSTGRES_IMG%.*}}

# PGBouncer Image
export PGBOUNCER_IMG=${PGBOUNCER_IMG:-$(grep 'DefaultPgbouncerImage.*=' "${ROOT_DIR}/pkg/versions/versions.go" | awk -F '"' '{print $2}' | tr -d '[:space:]')}

# MINIO Image
export MINIO_IMG=${MINIO_IMG:-$(grep 'minioImage.*=' "${ROOT_DIR}/tests/utils/minio/minio.go" | awk -F '"' '{print $2}' | tr -d '[:space:]')}

# Apache Image (Hardcoded stable default)
export APACHE_IMG=${APACHE_IMG:-"httpd"}

# Validate that required images were successfully extracted
if [ -z "${POSTGRES_IMG}" ]; then
  echo "ERROR: Failed to extract POSTGRES_IMG from ${ROOT_DIR}/pkg/versions/versions.go" >&2
  exit 1
fi
if [ -z "${PGBOUNCER_IMG}" ]; then
  echo "ERROR: Failed to extract PGBOUNCER_IMG from ${ROOT_DIR}/pkg/versions/versions.go" >&2
  exit 1
fi
if [ -z "${MINIO_IMG}" ]; then
  echo "ERROR: Failed to extract MINIO_IMG from ${ROOT_DIR}/tests/utils/minio/minio.go" >&2
  exit 1
fi

# Define the full array of helper images used by load-helper-images
HELPER_IMGS=("$POSTGRES_IMG" "$E2E_PRE_ROLLING_UPDATE_IMG" "$PGBOUNCER_IMG" "$MINIO_IMG" "$APACHE_IMG")
export HELPER_IMGS

# Testing the upgrade will require generating a second operator image, `-prime`
export TEST_UPGRADE_TO_V1=${TEST_UPGRADE_TO_V1:-true}

# Feature flags for enabling optional components
export ENABLE_PYROSCOPE=${ENABLE_PYROSCOPE:-}
export ENABLE_CSI_DRIVER=${ENABLE_CSI_DRIVER:-}
export ENABLE_APISERVER_AUDIT=${ENABLE_APISERVER_AUDIT:-}

# --- GENERIC ADD-ON CONSTANTS (Shared CSI/Snapshotter versions for Renovate) ---

# Define default CSI driver version
# renovate: datasource=github-releases depName=kubernetes-csi/csi-driver-host-path
CSI_DRIVER_HOST_PATH_DEFAULT_VERSION="v1.17.0"
# renovate: datasource=github-releases depName=kubernetes-csi/external-snapshotter
EXTERNAL_SNAPSHOTTER_VERSION="v8.4.0"
# renovate: datasource=github-releases depName=kubernetes-csi/external-provisioner
EXTERNAL_PROVISIONER_VERSION="v6.1.0"
# renovate: datasource=github-releases depName=kubernetes-csi/external-resizer
EXTERNAL_RESIZER_VERSION="v2.0.0"
# renovate: datasource=github-releases depName=kubernetes-csi/external-attacher
EXTERNAL_ATTACHER_VERSION="v4.10.0"

# Exporting CSI variables for use in setup scripts
export CSI_DRIVER_HOST_PATH_VERSION=${CSI_DRIVER_HOST_PATH_VERSION:-$CSI_DRIVER_HOST_PATH_DEFAULT_VERSION}
export EXTERNAL_SNAPSHOTTER_VERSION
export EXTERNAL_PROVISIONER_VERSION
export EXTERNAL_RESIZER_VERSION
export EXTERNAL_ATTACHER_VERSION
