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

# Standalone wrapper around install_barman_cloud_plugin so tests that manage
# their own operator deployment (e.g. the upgrade test, which recreates
# cnpg-system) can install cert-manager + plugin-barman-cloud after the operator
# is up. The plugin version is taken from BARMAN_PLUGIN_VERSION (default
# "release"); see install_barman_cloud_plugin in 20-utils-k8s.sh.

set -eEuo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../../"; pwd)

# install_barman_cloud_plugin relies on these being defined by the caller.
export K8S_CLI="${K8S_CLI:-kubectl}"
bright="${bright:-}"
reset="${reset:-}"

# shellcheck source=hack/testing-tools/common/20-utils-k8s.sh
source "${ROOT_DIR}/hack/testing-tools/common/20-utils-k8s.sh"

install_barman_cloud_plugin
