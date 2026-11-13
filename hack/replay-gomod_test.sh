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

## Tests for hack/replay-gomod.sh.
##
## Each test_* function builds its own throwaway git repository holding a root
## module and a `tests` module, records a commit that edits go.mod, then
## replays that commit onto a (possibly divergent) working tree and checks the
## result. Needs only git, go and jq; no network access.
##
## Usage: hack/replay-gomod_test.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPLAY="${SCRIPT_DIR}/replay-gomod.sh"

if ! command -v go >/dev/null 2>&1; then
    echo "skip: go binary not available" >&2
    exit 0
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "skip: jq binary not available" >&2
    exit 0
fi

TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0
declare -a TEST_FAILURES=()

RED='\033[0;31m'
GREEN='\033[0;32m'
BOLD='\033[1m'
NC='\033[0m'

export GIT_AUTHOR_NAME="Test User"
export GIT_AUTHOR_EMAIL="test@test.com"
export GIT_COMMITTER_NAME="Test User"
export GIT_COMMITTER_EMAIL="test@test.com"
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null
export GIT_CONFIG_COUNT=1
export GIT_CONFIG_KEY_0="init.defaultBranch"
export GIT_CONFIG_VALUE_0="main"

# ── Helpers ─────────────────────────────────────────────────────────

# assert_require <go.mod> <path> <version>
assert_require() {
    local file=$1 path=$2 expected=$3 actual
    actual=$(go mod edit -json "${file}" | jq -r --arg p "${path}" '.Require[]? | select(.Path == $p) | .Version')
    if [[ "${actual}" != "${expected}" ]]; then
        echo "ASSERTION FAILED: ${file}: expected ${path}@${expected}, got '${actual}'" >&2
        exit 1
    fi
}

# assert_no_require <go.mod> <path>
assert_no_require() {
    local file=$1 path=$2 actual
    actual=$(go mod edit -json "${file}" | jq -r --arg p "${path}" '.Require[]? | select(.Path == $p) | .Path')
    if [[ -n "${actual}" ]]; then
        echo "ASSERTION FAILED: ${file}: expected ${path} to be absent, but found it" >&2
        exit 1
    fi
}

# assert_go_directive <go.mod> <version>
assert_go_directive() {
    local file=$1 expected=$2 actual
    actual=$(go mod edit -json "${file}" | jq -r '.Go // ""')
    if [[ "${actual}" != "${expected}" ]]; then
        echo "ASSERTION FAILED: ${file}: expected go=${expected}, got '${actual}'" >&2
        exit 1
    fi
}

# assert_toolchain <go.mod> <toolchain> (empty string means no toolchain)
assert_toolchain() {
    local file=$1 expected=$2 actual
    actual=$(go mod edit -json "${file}" | jq -r '.Toolchain // ""')
    if [[ "${actual}" != "${expected}" ]]; then
        echo "ASSERTION FAILED: ${file}: expected toolchain='${expected}', got '${actual}'" >&2
        exit 1
    fi
}

# assert_replace <go.mod> <old> <new>
assert_replace() {
    local file=$1 old=$2 new=$3 actual
    actual=$(go mod edit -json "${file}" | jq -r --arg o "${old}" '.Replace[]? | select(.Old.Path == $o) | .New.Path')
    if [[ "${actual}" != "${new}" ]]; then
        echo "ASSERTION FAILED: ${file}: expected replace ${old} => ${new}, got '${actual}'" >&2
        exit 1
    fi
}

# write_gomod <file> <go-version> [<path> <version>]...
write_gomod() {
    local target=$1 gover=$2
    shift 2
    {
        echo "module example.com/test"
        echo
        echo "go ${gover}"
        while [[ $# -ge 2 ]]; do
            echo
            echo "require $1 $2"
            shift 2
        done
    } > "${target}"
}

# init_repo: initialise a repository with a root and a `tests` module.
init_repo() {
    git init --quiet
    mkdir -p tests
}

# commit_all <message>: commit everything and set PICKED to the new commit.
commit_all() {
    git add -A
    git commit --quiet -m "$1"
    PICKED=$(git rev-parse HEAD)
}

# assert_unchanged <file> <reference-copy> <message>
assert_unchanged() {
    if ! cmp -s "$1" "$2"; then
        echo "ASSERTION FAILED: $3" >&2
        return 1
    fi
}

run_test() {
    local name=$1 test_dir output rc=0
    TESTS_RUN=$((TESTS_RUN + 1))
    test_dir=$(mktemp -d)
    echo -ne "  ${name} ... "
    if output=$("${name}" "${test_dir}" 2>&1); then
        echo -e "${GREEN}PASS${NC}"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        rc=$?
        echo -e "${RED}FAIL${NC}"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        TEST_FAILURES+=("${name} (rc=${rc}): ${output}")
    fi
    rm -rf "${test_dir}"
}

# ── Root module tests ───────────────────────────────────────────────

# Commit adds a require. Replay onto a divergent tree must re-add it.
test_root_adds_require() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/keep v1.0.0
    commit_all base

    write_gomod go.mod 1.21 example.com/keep v1.0.0 example.com/added v0.5.0
    commit_all "add example.com/added"

    write_gomod go.mod 1.21 example.com/keep v1.0.0 example.com/other v0.2.0
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_require go.mod example.com/added v0.5.0
    assert_require go.mod example.com/other v0.2.0 # untouched
    assert_require go.mod example.com/keep v1.0.0  # untouched
}

# Commit bumps a require. Replay applies the new version.
test_root_changes_version() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/dep v1.0.0
    commit_all base

    write_gomod go.mod 1.21 example.com/dep v1.3.0
    commit_all "bump dep"

    write_gomod go.mod 1.21 example.com/dep v1.1.0 example.com/extra v0.1.0
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_require go.mod example.com/dep v1.3.0
    assert_require go.mod example.com/extra v0.1.0
}

# Commit drops a require. Replay removes it from the working tree.
test_root_drops_require() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/keep v1.0.0 example.com/remove v0.5.0
    commit_all base

    write_gomod go.mod 1.21 example.com/keep v1.0.0
    commit_all "drop example.com/remove"

    write_gomod go.mod 1.21 example.com/keep v1.0.0 example.com/remove v0.5.0 example.com/extra v0.1.0
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_no_require go.mod example.com/remove
    assert_require go.mod example.com/extra v0.1.0
    assert_require go.mod example.com/keep v1.0.0
}

# Commit bumps the `go` directive. Replay applies it.
test_root_bumps_go_directive() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21
    commit_all base

    write_gomod go.mod 1.23
    commit_all "bump go directive"

    write_gomod go.mod 1.22
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_go_directive go.mod 1.23
}

# Commit adds a toolchain. Replay applies it to a tree without one.
test_root_adds_toolchain() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.22
    commit_all base

    go mod edit -toolchain=go1.23.5 go.mod
    commit_all "add toolchain"

    write_gomod go.mod 1.22
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_toolchain go.mod go1.23.5
}

# Commit drops the toolchain. Replay removes it from the working tree.
test_root_drops_toolchain() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.22
    go mod edit -toolchain=go1.22.0 go.mod
    commit_all base

    go mod edit -toolchain=none go.mod
    commit_all "drop toolchain"

    write_gomod go.mod 1.22
    go mod edit -toolchain=go1.22.0 go.mod
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_toolchain go.mod ""
}

# Commit doesn't touch go.mod. Replay is a no-op and exits 0.
test_root_noop_when_commit_does_not_touch_gomod() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/keep v1.0.0
    commit_all base

    echo unrelated > other.txt
    commit_all "unrelated change"

    # Divergent go.mod in the working tree; replay must leave it alone.
    write_gomod go.mod 1.21 example.com/keep v1.0.0 example.com/extra v0.1.0
    cp go.mod .before
    local out
    out=$("${REPLAY}" "${PICKED}" 2>&1)

    assert_unchanged go.mod .before "replay modified go.mod although the commit did not touch it"
    [[ "${out}" == *"nothing to replay"* ]] || { echo "ASSERTION FAILED: missing 'nothing to replay' message: ${out}" >&2; return 1; }
}

# Re-applying a commit whose intent is already in the tree changes nothing.
test_root_idempotent_when_already_applied() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/keep v1.0.0
    commit_all base

    write_gomod go.mod 1.22 example.com/keep v1.0.0 example.com/added v0.5.0
    commit_all "add example.com/added"

    cp go.mod .before
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_unchanged go.mod .before "replay was not idempotent"
}

# Replaying the root module must not touch tests/go.mod.
test_root_leaves_tests_gomod_untouched() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/dep v1.0.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    commit_all base

    write_gomod go.mod 1.21 example.com/dep v1.1.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.1.0
    commit_all "bump dep in both modules"

    write_gomod go.mod 1.21 example.com/dep v1.0.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    "${REPLAY}" "${PICKED}" >/dev/null

    assert_require go.mod example.com/dep v1.1.0
    assert_require tests/go.mod example.com/dep v1.0.0
}

# ── tests module tests ──────────────────────────────────────────────

# Commit adds a require to tests/go.mod. Replay must re-add it.
test_tests_adds_require() {
    cd "$1"
    init_repo
    write_gomod tests/go.mod 1.21 example.com/keep v1.0.0
    commit_all base

    write_gomod tests/go.mod 1.21 example.com/keep v1.0.0 example.com/added v0.5.0
    commit_all "add example.com/added"

    write_gomod tests/go.mod 1.21 example.com/keep v1.0.0 example.com/other v0.2.0
    "${REPLAY}" "${PICKED}" tests >/dev/null

    assert_require tests/go.mod example.com/added v0.5.0
    assert_require tests/go.mod example.com/other v0.2.0 # untouched
    assert_require tests/go.mod example.com/keep v1.0.0  # untouched
}

# The scenario the module-dir argument exists for: both modules bump a
# dependency, the release branch's tests/go.mod stays on the old version.
test_tests_changes_version() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/dep v1.0.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    commit_all base

    write_gomod go.mod 1.21 example.com/dep v1.3.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.3.0
    commit_all "bump dep in both modules"

    write_gomod go.mod 1.21 example.com/dep v1.1.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.1.0 example.com/extra v0.1.0
    "${REPLAY}" "${PICKED}" >/dev/null
    "${REPLAY}" "${PICKED}" tests >/dev/null

    assert_require go.mod example.com/dep v1.3.0
    assert_require tests/go.mod example.com/dep v1.3.0
    assert_require tests/go.mod example.com/extra v0.1.0
}

# Commit drops a require from tests/go.mod. Replay removes it.
test_tests_drops_require() {
    cd "$1"
    init_repo
    write_gomod tests/go.mod 1.21 example.com/keep v1.0.0 example.com/remove v0.5.0
    commit_all base

    write_gomod tests/go.mod 1.21 example.com/keep v1.0.0
    commit_all "drop example.com/remove"

    write_gomod tests/go.mod 1.21 example.com/keep v1.0.0 example.com/remove v0.5.0 example.com/extra v0.1.0
    "${REPLAY}" "${PICKED}" tests >/dev/null

    assert_no_require tests/go.mod example.com/remove
    assert_require tests/go.mod example.com/extra v0.1.0
    assert_require tests/go.mod example.com/keep v1.0.0
}

# Commit bumps the `go` directive of tests/go.mod. Replay applies it.
test_tests_bumps_go_directive() {
    cd "$1"
    init_repo
    write_gomod tests/go.mod 1.21
    commit_all base

    write_gomod tests/go.mod 1.23
    commit_all "bump go directive"

    write_gomod tests/go.mod 1.22
    "${REPLAY}" "${PICKED}" tests >/dev/null

    assert_go_directive tests/go.mod 1.23
}

# Commit adds a toolchain to tests/go.mod. Replay applies it.
test_tests_adds_toolchain() {
    cd "$1"
    init_repo
    write_gomod tests/go.mod 1.22
    commit_all base

    go mod edit -toolchain=go1.23.5 tests/go.mod
    commit_all "add toolchain"

    write_gomod tests/go.mod 1.22
    "${REPLAY}" "${PICKED}" tests >/dev/null

    assert_toolchain tests/go.mod go1.23.5
}

# The replace directive that points tests/ at the root module must survive.
test_tests_preserves_replace_directive() {
    cd "$1"
    init_repo
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    go mod edit -replace=example.com/test=../ tests/go.mod
    commit_all base

    go mod edit -require=example.com/dep@v1.1.0 tests/go.mod
    commit_all "bump dep"

    go mod edit -require=example.com/dep@v1.0.0 tests/go.mod
    "${REPLAY}" "${PICKED}" tests >/dev/null

    assert_require tests/go.mod example.com/dep v1.1.0
    assert_replace tests/go.mod example.com/test ../
}

# Commit touches only the root go.mod. Replaying `tests` is a no-op.
test_tests_noop_when_commit_does_not_touch_tests_gomod() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/dep v1.0.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    commit_all base

    write_gomod go.mod 1.21 example.com/dep v1.1.0
    commit_all "bump dep in root only"

    cp tests/go.mod .before
    local out
    out=$("${REPLAY}" "${PICKED}" tests 2>&1)

    assert_unchanged tests/go.mod .before "replay modified tests/go.mod although the commit did not touch it"
    [[ "${out}" == *"nothing to replay"* ]] || { echo "ASSERTION FAILED: missing 'nothing to replay' message: ${out}" >&2; return 1; }
}

# Replaying `tests` must not touch the root go.mod.
test_tests_leaves_root_gomod_untouched() {
    cd "$1"
    init_repo
    write_gomod go.mod 1.21 example.com/dep v1.0.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    commit_all base

    write_gomod go.mod 1.21 example.com/dep v1.1.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.1.0
    commit_all "bump dep in both modules"

    write_gomod go.mod 1.21 example.com/dep v1.0.0
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    "${REPLAY}" "${PICKED}" tests >/dev/null

    assert_require tests/go.mod example.com/dep v1.1.0
    assert_require go.mod example.com/dep v1.0.0
}

# ── Argument handling tests ─────────────────────────────────────────

# A trailing slash on module-dir is accepted.
test_module_dir_accepts_trailing_slash() {
    cd "$1"
    init_repo
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    commit_all base

    write_gomod tests/go.mod 1.21 example.com/dep v1.1.0
    commit_all "bump dep"

    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    "${REPLAY}" "${PICKED}" tests/ >/dev/null

    assert_require tests/go.mod example.com/dep v1.1.0
}

# A leading ./ on module-dir is accepted.
test_module_dir_accepts_dot_slash_prefix() {
    cd "$1"
    init_repo
    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    commit_all base

    write_gomod tests/go.mod 1.21 example.com/dep v1.1.0
    commit_all "bump dep"

    write_gomod tests/go.mod 1.21 example.com/dep v1.0.0
    "${REPLAY}" "${PICKED}" ./tests >/dev/null

    assert_require tests/go.mod example.com/dep v1.1.0
}

# No arguments is a usage error.
test_rejects_no_arguments() {
    cd "$1"
    init_repo
    if "${REPLAY}" >/dev/null 2>&1; then
        echo "ASSERTION FAILED: expected a non-zero exit with no arguments" >&2
        return 1
    fi
}

# More than two arguments is a usage error.
test_rejects_too_many_arguments() {
    cd "$1"
    init_repo
    if "${REPLAY}" a b c >/dev/null 2>&1; then
        echo "ASSERTION FAILED: expected a non-zero exit with three arguments" >&2
        return 1
    fi
}

# ── Main ────────────────────────────────────────────────────────────

echo -e "${BOLD}Running tests for hack/replay-gomod.sh${NC}"
echo ""

run_test test_root_adds_require
run_test test_root_changes_version
run_test test_root_drops_require
run_test test_root_bumps_go_directive
run_test test_root_adds_toolchain
run_test test_root_drops_toolchain
run_test test_root_noop_when_commit_does_not_touch_gomod
run_test test_root_idempotent_when_already_applied
run_test test_root_leaves_tests_gomod_untouched

run_test test_tests_adds_require
run_test test_tests_changes_version
run_test test_tests_drops_require
run_test test_tests_bumps_go_directive
run_test test_tests_adds_toolchain
run_test test_tests_preserves_replace_directive
run_test test_tests_noop_when_commit_does_not_touch_tests_gomod
run_test test_tests_leaves_root_gomod_untouched

run_test test_module_dir_accepts_trailing_slash
run_test test_module_dir_accepts_dot_slash_prefix
run_test test_rejects_no_arguments
run_test test_rejects_too_many_arguments

echo ""
echo "Tests run: ${TESTS_RUN}, passed: ${TESTS_PASSED}, failed: ${TESTS_FAILED}"
if [[ ${TESTS_FAILED} -gt 0 ]]; then
    printf '%s\n' "${TEST_FAILURES[@]}" >&2
    exit 1
fi
