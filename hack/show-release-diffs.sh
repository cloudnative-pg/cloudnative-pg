#!/usr/bin/env bash
##
## CloudNativePG - Show diffs from main for a release branch
##
## This is a helper script that prints the GitHub pull requests
## that are present in the trunk and not in this branch, and
## viceversa. It should be used to help maintainers spot any
## missed commit to be backported, while waiting for an automate
## procedure that issues PRs on all supported release branches.
##
## You need to run this script from the release branch. For example
##
##     git checkout release-1.16
##     ./hack/show-release-diffs.sh
##
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

if [ "$#" -ne 0 ]; then
    echo "Usage: hack/show-release-diffs.sh"
    exit 1
fi

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

_output_intermediate_csv () {
    BRANCH=$1
    while read -r r
    do
        ID=$(echo "$r" | cut -f 2 -d '[' | cut -f 1 -d ']')
        MSG=$(echo "$r" | cut -f 2 -d ']')

        # Skip lines that don't end with ')'
        if [[ $MSG =~ \)$ ]]; then
            PR=$(echo "$r" | rev | cut -f 1 -d '(' | rev | cut -f 1 -d ')' | cut -f 2 -d '#')
            # Check PR is a number
            if ! [[ "$PR" -eq "$PR" ]] 2> /dev/null; then
               PR='-'
            fi
        else
          PR='-'
        fi
        echo "${PR}|${ID}|${MSG## }"
    done < <(grep -e "\[${BRANCH}[0-9~]*\]" "$WORKDIR/show-branch.txt")
}

# Require to be in a release branch
require_clean_work_tree "release"

# Verify that you are in a release branch
if branch=$(git symbolic-ref --short -q HEAD) && [[ "$branch" == release-* ]]
then
    echo "# Checking ${branch} vs main"
else
    echo >&2 "You must be on a 'release-*' branch ($branch) to run differences with main"
    exit 1
fi

WORKDIR=$(mktemp -d -t cnpg-XXXX)
trap 'rm -rf "$WORKDIR"' EXIT

git show-branch --current main > "$WORKDIR/show-branch.txt"
_output_intermediate_csv main | sort -nu > "$WORKDIR/main.csv"
_output_intermediate_csv "$branch" | sort -nu > "$WORKDIR/$branch.csv"

# Prepare intermediate files
cut -f 1 -d '|' "$WORKDIR/main.csv" > "$WORKDIR/main-PR.txt"
cut -f 1 -d '|' "$WORKDIR/$branch.csv" > "$WORKDIR/$branch-PR.txt"
grep '^-|' "$WORKDIR/main.csv" | cut -f 3 -d '|' | sort -u > "$WORKDIR/main-noPR.txt"
grep '^-|' "$WORKDIR/$branch.csv" | cut -f 3 -d '|' | sort -u > "$WORKDIR/$branch-noPR.txt"
diff -B "$WORKDIR/main-noPR.txt" "$WORKDIR/$branch-noPR.txt" > "$WORKDIR/manual-verification.txt"

# What's missing in the release
echo -e "\n## PRs that are missing in $branch but are in main\n"
i=0
while read -r pr
do
    ((i=i+1))
    MSG=$(grep "^$pr|" "$WORKDIR/main.csv" | cut -f 3 -d '|')
    echo "$i. [$MSG](https://github.com/cloudnative-pg/cloudnative-pg/pull/$pr)"
done < <(diff -B "$WORKDIR/main-PR.txt" "$WORKDIR/$branch-PR.txt" | grep '^<' | cut -f 2 -d ' ')

# What's in the release and not in main
echo -e "\n## PRs that are in $branch but not in main\n"
i=0
while read -r pr
do
    ((i=i+1))
    MSG=$(grep "^$pr|" "$WORKDIR/$branch.csv" | cut -f 3 -d '|')
    echo "$i. [$MSG](https://github.com/cloudnative-pg/cloudnative-pg/pull/$pr)"
done < <(diff -B "$WORKDIR/main-PR.txt" "$WORKDIR/$branch-PR.txt" | grep '^>' | cut -f 2 -d ' ')

# Verify commits without a PR
if [ -s "$WORKDIR/manual-verification.txt" ]
then
    echo -e "\n## Commits without a PR - please check\n"
    cat "$WORKDIR/manual-verification.txt"
fi
