#!/usr/bin/env bash
##
## Copyright The CloudNativePG Contributors
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

set -o errexit -o nounset -o pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

if [ "$#" -ne 1 ]; then
    echo "Usage: hack/release.sh release-version"
    exit 1
fi

release_version=${1#v}

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

if branch=$(git symbolic-ref --short -q HEAD) && [ "$branch" = 'main' ]
then
    echo "Releasing ${release_version}"
else
    echo >&2 "Release is not possible because you are not on 'main' branch ($branch)"
    exit 1
fi

make kustomize
KUSTOMIZE="${REPO_ROOT}/bin/kustomize"

mkdir -p releases/
release_manifest="releases/postgresql-operator-${release_version}.yaml"

sed -i -e "/Version *= *.*/Is/\".*\"/\"${release_version}\"/" \
    -e "/DefaultOperatorImageName *= *.*/Is/\"\(.*\):.*\"/\"\1:${release_version}\"/" \
    pkg/versions/versions.go

sed -i "s/postgresql-operator-[0-9.]*.yaml/postgresql-operator-${release_version}.yaml/g" \
    docs/src/installation_upgrade.md

CONFIG_TMP_DIR=$(mktemp -d)
cp -r config/* "${CONFIG_TMP_DIR}"
(
    cd "${CONFIG_TMP_DIR}/manager"
    "${KUSTOMIZE}" edit set image controller="quay.io/enterprisedb/cloud-native-postgresql:${release_version}"
)

"${KUSTOMIZE}" build "${CONFIG_TMP_DIR}/default" > "${release_manifest}"
rm -fr "${CONFIG_TMP_DIR}"

git checkout -b "release/v${release_version}"
git add \
    pkg/versions/versions.go \
    docs/src/installation_upgrade.md \
    "${release_manifest}"
git commit -sm "Version tag to ${release_version}"
git push origin -u "release/v${release_version}"

cat <<EOF
Generated release manifest ${release_manifest}
Created and pushed branch release/v${release_version}
EOF
