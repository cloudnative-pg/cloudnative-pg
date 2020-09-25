#!/usr/bin/env bash
##
## This file is part of Cloud Native PostgreSQL.
##
## Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
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

if branch=$(git symbolic-ref --short -q HEAD) && [ $branch = 'master' ]
then
    echo "Releasing ${release_version}"
else
    echo >&2 "Release is not possible because you are not on 'master' branch ($branch)"
    exit 1
fi

if [ -z "$(go env GOBIN)" ]
then
    GOBIN="$(go env GOPATH)/bin"
else
    GOBIN=$( go env GOBIN)
fi

make kustomize
KUSTOMIZE=$(PATH="${GOBIN}:${PATH}" which kustomize)

mkdir -p releases/
release_manifest="releases/postgresql-operator-${release_version}.yaml"

sed -i -e "/Version *= *.*/Is/\".*\"/\"${release_version}\"/" \
    -e "/DefaultOperatorImageName *= *.*/Is/\"\(.*\):.*\"/\"\1:v${release_version}\"/" \
    pkg/versions/versions.go

sed -i -e "s/version=\".*\"/version=\"${release_version}\"/" \
    Dockerfile

CONFIG_TMP_DIR=$(mktemp -d)
cp -r config/* "${CONFIG_TMP_DIR}"
(
    cd "${CONFIG_TMP_DIR}/default"
    "${KUSTOMIZE}" edit add patch manager_image_pull_secret.yaml
    cd "${CONFIG_TMP_DIR}/manager"
    "${KUSTOMIZE}" edit set image controller="quay.io/2ndquadrant/cloud-native-postgresql:v${release_version}"
)

"${KUSTOMIZE}" build "${CONFIG_TMP_DIR}/default" > "${release_manifest}"
rm -fr "${CONFIG_TMP_DIR}"

git add \
    pkg/versions/versions.go \
    Dockerfile \
    "${release_manifest}"
git commit -sm "Version tag to ${release_version}"
git tag -sam "Release ${release_version}" "v${release_version}"

cat <<EOF
Generated release manifest ${release_manifest}
Tagged version ${release_version}

Remember to push tags as well:

git push origin master v${release_version}
EOF
