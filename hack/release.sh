#!/usr/bin/env bash
##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2021 EnterpriseDB Corporation.
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

if [ -z "$(go env GOBIN)" ]
then
    GOBIN="$(go env GOPATH)/bin"
else
    GOBIN=$( go env GOBIN)
fi

make kustomize
KUSTOMIZE=$(PATH="${GOBIN}:${PATH}" command -v kustomize)

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
