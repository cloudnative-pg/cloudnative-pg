#!/usr/bin/env bash
set -euo pipefail

echo "🔍 CloudNativePG Development Environment Requirements Check"
echo ""

printf "|-%-20s-|-%-7s-|-%-45s-|\n" "--------------------" "-------" "---------------------------------------------"
printf "| %-20s | %-7s | %-45s |\n" "Requirement" "Status" "Details"
printf "|-%-20s-|-%-7s-|-%-45s-|\n" "--------------------" "-------" "---------------------------------------------"

exit_code=0

print_result() {
    local name="$1"
    local status_icon="$2"
    local details="$3"

    case "$status_icon" in
        "✅" | "❌")
            printf "| %-20s | %-8s | %-45s |\n" "$name" "$status_icon" "$details"
            ;;
        "🟡")
            printf "| %-20s | %-9s | %-45s |\n" "$name" "🟡" "$details"
            ;;
    esac

    if [[ "$status_icon" == "❌" ]]; then
        exit_code=1
    fi
}

version_gte() {
    [ "$(printf '%s\n' "$1" "$2" | sort -V | head -n1)" = "$2" ]
}

check_tool() {
    local name="$1"
    local cmd="$2"
    local min_version="${3:-}"
    local optional="${4:-false}"

    if ! command -v "$cmd" >/dev/null 2>&1; then
        if [ "$optional" = true ]; then
            print_result "$name" "🟡" "Not installed (optional)"
        else
            if [ "$optional" = true ]; then
                print_result "$name" "🟡" "Version $current_version < $min_version (optional)"
            else
                print_result "$name" "❌" "Version $current_version is old (need $min_version+)"
            fi
        fi
        return
    fi

    local current_version=""
    case "$cmd" in
        go) current_version=$(go version | grep -oE 'go[0-9]+\.[0-9]+(\.[0-9]+)?' | sed 's/go//');;
        make) current_version=$(make --version | head -n1 | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1);;
        kind) current_version=$(kind version | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n1 | sed 's/v//');;
        golangci-lint) current_version=$(golangci-lint version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1);;
        goreleaser) current_version=$(goreleaser --version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1);;
        operator-sdk) current_version=$(operator-sdk version | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n1 | sed 's/v//');;
        docker) current_version=$(docker --version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1);;
        podman) current_version=$(podman --version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1);;
        git) current_version=$(git --version | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1);;
        gpg) current_version=$(gpg --version | head -n1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1);;
        jq) current_version=$(jq --version | grep -oE '[0-9]+\.[0-9]+' | head -n1);;
        pandoc) current_version=$(pandoc --version | head -n1 | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1);;
        tar) current_version=$(tar --version 2>/dev/null | head -n1 | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1);;
        sed) current_version=$(sed --version 2>/dev/null | head -n1 | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1);;
        find) current_version=$(find --version 2>/dev/null | head -n1 | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1);;
        diff) current_version=$(diff --version 2>/dev/null | head -n1 | grep -oE '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1);;
        kubectl) current_version=$(kubectl version --client 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -n1 2>/dev/null || echo "unknown");;
        *)
            print_result "$name" "✅" "Installed"
            return
            ;;
    esac

    if [ -z "$current_version" ] || [ "$current_version" = "unknown" ]; then
        print_result "$name" "✅" "Installed (version unknown)"
        return
    fi

    if [ -n "$min_version" ]; then
        if version_gte "$current_version" "$min_version"; then
            print_result "$name" "✅" "Version $current_version"
        else
            print_result "$name" "❌" "Version $current_version is old (need $min_version+)"
        fi
    else
        print_result "$name" "✅" "Version $current_version"
    fi
}

check_tool "Go (1.21+)" "go" "1.21"
check_tool "GNU Make" "make"
check_tool "Kind (0.20+)" "kind" "0.20.0"
check_tool "golangci-lint" "golangci-lint"
check_tool "goreleaser" "goreleaser"
check_tool "operator-sdk" "operator-sdk"

if command -v docker >/dev/null 2>&1; then
    check_tool "Container Runtime" "docker"
elif command -v podman >/dev/null 2>&1; then
    check_tool "Container Runtime" "podman"
else
    print_result "Container Runtime" "❌" "Neither Docker nor Podman found"
fi

check_tool "Git" "git"
check_tool "GPG" "gpg"
check_tool "jq" "jq"
check_tool "pandoc" "pandoc"
check_tool "Tar" "tar"
check_tool "Sed" "sed"
check_tool "Find" "find"
check_tool "Diff" "diff"

if git config user.name >/dev/null 2>&1 && git config user.email >/dev/null 2>&1; then
    user_name=$(git config user.name)
    user_email=$(git config user.email)
    print_result "Git Config" "✅" "$user_name <$user_email>"
else
    print_result "Git Config" "❌" "user.name/email not configured"
fi

if [ -z "${GOPATH:-}" ]; then
    print_result "GOPATH" "🟡" "Not set (using Go modules)"
else
    print_result "GOPATH" "✅" "$GOPATH"
fi

check_tool "kubectl" "kubectl" "" "true"

if command -v df >/dev/null 2>&1; then
    available_space=$(df -h . 2>/dev/null | awk 'NR==2 {print $4}' 2>/dev/null || echo "unknown")
    if [ "$available_space" != "unknown" ]; then
        print_result "Disk Space" "✅" "$available_space available"
    else
        print_result "Disk Space" "🟡" "Cannot determine"
    fi
else
    print_result "Disk Space" "🟡" "df command not available"
fi

printf "|-%-20s-|-%-7s-|-%-45s-|\n" "--------------------" "-------" "---------------------------------------------"
echo ""
exit $exit_code
