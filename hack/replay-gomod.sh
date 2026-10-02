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

##
## Replay a commit's go.mod intent onto the current working tree.
##
## When cherry-picking a commit that changes go.mod and the release branch
## has diverged on context, git's textual merge gives up or produces noisy
## output. This script applies the picked commit's go.mod operations
## semantically (via `go mod edit`), regardless of line context:
##
##   - Apply the `go` directive if the commit changed it
##   - Apply the `toolchain` directive if the commit changed it
##   - Apply each require addition or version change
##   - Drop requires the commit removed
##
## Used by the backport workflow and available for maintainers finishing a
## backport by hand. Does not regenerate go.sum; run `go mod tidy` and
## `go mod verify` afterwards to settle hashes.
##
## Usage: hack/replay-gomod.sh <commit-sha>

set -euo pipefail

if [ $# -ne 1 ]; then
    echo "Usage: $0 <commit-sha>" >&2
    exit 1
fi

commit=$1

if ! git diff-tree --no-commit-id --name-only -r "${commit}" | grep -qE '^go\.mod$'; then
    echo "Commit ${commit} does not touch go.mod; nothing to replay." >&2
    exit 0
fi

PARENT_GO_MOD=$(mktemp)
PICKED_GO_MOD=$(mktemp)
trap 'rm -f "${PARENT_GO_MOD}" "${PICKED_GO_MOD}"' EXIT

git show "${commit}^:go.mod" > "${PARENT_GO_MOD}"
git show "${commit}:go.mod"  > "${PICKED_GO_MOD}"

# go directive
PICKED_GO=$(go mod edit -json "${PICKED_GO_MOD}" | jq -r '.Go // ""')
PARENT_GO=$(go mod edit -json "${PARENT_GO_MOD}" | jq -r '.Go // ""')
if [ "${PICKED_GO}" != "${PARENT_GO}" ] && [ -n "${PICKED_GO}" ]; then
    go mod edit -go="${PICKED_GO}"
fi

# toolchain directive
PICKED_TC=$(go mod edit -json "${PICKED_GO_MOD}" | jq -r '.Toolchain // ""')
PARENT_TC=$(go mod edit -json "${PARENT_GO_MOD}" | jq -r '.Toolchain // ""')
if [ "${PICKED_TC}" != "${PARENT_TC}" ]; then
    if [ -n "${PICKED_TC}" ]; then
        go mod edit -toolchain="${PICKED_TC}"
    else
        go mod edit -droptoolchain
    fi
fi

# require directives: additions and version changes
EXTRACT='[.Require[]? | "\(.Path)@\(.Version)"] | sort | .[]'
while IFS= read -r req; do
    [ -z "${req}" ] && continue
    go mod edit -require="${req}"
done < <(comm -23 \
    <(go mod edit -json "${PICKED_GO_MOD}" | jq -r "${EXTRACT}") \
    <(go mod edit -json "${PARENT_GO_MOD}" | jq -r "${EXTRACT}"))

# require directives: removals
EXTRACT_PATH='[.Require[]? | .Path] | sort | unique | .[]'
while IFS= read -r path; do
    [ -z "${path}" ] && continue
    go mod edit -droprequire="${path}" 2>/dev/null || true
done < <(comm -23 \
    <(go mod edit -json "${PARENT_GO_MOD}" | jq -r "${EXTRACT_PATH}") \
    <(go mod edit -json "${PICKED_GO_MOD}" | jq -r "${EXTRACT_PATH}"))
