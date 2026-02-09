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

## CloudNativePG release script
##
## This shell script automates the release process for a selected
## version of CloudNativePG. It must be invoked from a release
## branch. For details on the release procedure, please
## refer to the contribute/release_procedure.md file from the
## main folder.
##

set -o errexit -o nounset -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

if [ "$#" -ne 1 ]; then
    echo "Usage: hack/release.sh release-version"
    exit 1
fi

release_version=${1#v}

# Verify we are working on a clean directory
require_clean_work_tree () {
    git rev-parse --verify HEAD >/dev/null || exit 1
    git update-index -q --ignore-submodules --refresh
    err=0

    if ! git diff-files --quiet --ignore-submodules
    then
        echo >&2 "Cannot $1: You have unstaged changes."
        err=1
    fi

    if ! git diff-index --cached --quiet --ignore-submodules HEAD --
    then
        if [ $err = 0 ]
        then
            echo >&2 "Cannot $1: Your index contains uncommitted changes."
        else
            echo >&2 "Additionally, your index contains uncommitted changes."
        fi
        err=1
    fi

    if [ $err = 1 ]
    then
        # if there is a 2nd argument print it
        test -n "${2+1}" && echo >&2 "$2"
        exit 1
    fi
}

require_clean_work_tree "release"

# Verify that you are in a proper branch
# Releases can only be triggered from:
# - a release branch (for stable releases)
# - main (for release candidate only)
branch=$(git symbolic-ref --short -q HEAD)
case $branch in
  release-*)
    echo "Releasing ${release_version}"
    ;;
  main)
    if [[ "${release_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
    then
        echo >&2 "Cannot release a stable version from 'main'"
        exit 1
    fi
    echo "Releasing ${release_version}"
    ;;
  *)
    echo >&2 "Release is not possible because you are not on 'main' or a 'release-*' branch ($branch)"
    exit 1
    ;;
esac

make kustomize
KUSTOMIZE="${REPO_ROOT}/bin/kustomize"

mkdir -p releases/
release_manifest="releases/cnpg-${release_version}.yaml"
# shellcheck disable=SC2001
release_branch="release-$(sed -e 's/^\([0-9]\+\.[0-9]\+\)\..*$/\1/' <<< "$release_version" )"

# Perform automated substitutions of the version string in the source code
sed -i -e "/Version *= *.*/Is/\".*\"/\"${release_version}\"/" \
    -e "/DefaultOperatorImageName *= *.*/Is/\"\(.*\):.*\"/\"\1:${release_version}\"/" \
    pkg/versions/versions.go

sed -i -e "s@\(release-[0-9.]\+\|main\)/releases/cnpg-[0-9.]\+\(-rc.*\)\?.yaml@${branch}/releases/cnpg-${release_version}.yaml@g" \
    -e "s@artifacts/release-[0-9.]*/@artifacts/${release_branch}/@g" \
    docs/src/installation_upgrade.md

sed -i -e "s@1\.[0-9]\+\.[0-9]\+\(-[a-z][a-z0-9]*\)\?@${release_version}@g" docs/src/kubectl-plugin.md

CONFIG_TMP_DIR=$(mktemp -d)
cp -r config/* "${CONFIG_TMP_DIR}"
(
    cd "${CONFIG_TMP_DIR}/manager"
    "${KUSTOMIZE}" edit set image controller="ghcr.io/cloudnative-pg/cloudnative-pg:${release_version}"
)

"${KUSTOMIZE}" build "${CONFIG_TMP_DIR}/default" > "${release_manifest}"
rm -fr "${CONFIG_TMP_DIR}"

# Create a new branch for the release that originates the PR
git checkout -b "release/v${release_version}"
git add \
    pkg/versions/versions.go \
    docs/src/installation_upgrade.md \
    docs/src/kubectl-plugin.md \
    "${release_manifest}"
git commit -sm "Version tag to ${release_version}"
git push origin -u "release/v${release_version}"

cat <<EOF
Generated release manifest ${release_manifest}
Created and pushed branch release/v${release_version}
EOF
