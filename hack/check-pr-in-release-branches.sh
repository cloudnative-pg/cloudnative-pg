#!/usr/bin/env bash
#
# check-pr-in-release-branches.sh <string>
#
# Example: ./hack/check-pr-in-release-branches.sh #7988

set -euo pipefail

if [ $# -lt 1 ]; then
    echo "Usage: $0 <string>"
    exit 1
fi

search_string="$1"
shift

branches="main $(git for-each-ref --format '%(refname)' 'refs/heads/release*' | sed -e 's@refs/heads/@@' | sort -rV)"
found=0
for branch in $branches; do
    echo "Checking branch: $branch"
    if git log "origin/$branch" --grep="$search_string" -i --oneline | grep -q .; then
        echo "✅ Found \"$search_string\" in commits on branch: $branch"
        found=1
    else
        echo "❌ \"$search_string\" not found in commits on branch: $branch"
    fi
done

if [ $found -eq 0 ]; then
    echo "String \"$search_string\" not found in any specified branches."
    exit 1
fi
