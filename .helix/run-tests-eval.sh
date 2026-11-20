#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

DOCKERFILE="$SCRIPT_DIR/Dockerfile.helix"

die()  { echo "Error: $*" >&2; exit 1; }
info() { echo "$*" >&2; }

# ── Parse test command from Dockerfile.helix (CMD or ENTRYPOINT) ──

parse_instruction() {
    local raw="$1"
    if [[ "$raw" == \[* ]]; then
        local inner="${raw#\[}"
        inner="${inner%\]}"
        local result="" elem=""
        while IFS= read -r -d ',' elem || [[ -n "$elem" ]]; do
            elem=$(echo "$elem" | sed 's/^[[:space:]]*"//; s/"[[:space:]]*$//')
            [[ -z "$elem" ]] && continue
            if [[ "$elem" == *" "* ]]; then
                elem="${elem//\\/\\\\}"
                elem="${elem//\"/\\\"}"
                result+="\"${elem}\" "
            else
                result+="$elem "
            fi
        done <<< "$inner"
        echo "$result" | sed 's/ *$//'
    else
        echo "$raw"
    fi
}

extract_cmd() {
    local cmd_line="" ep_line=""
    while IFS= read -r line || [[ -n "$line" ]]; do
        local trimmed="${line#"${line%%[![:space:]]*}"}"
        case "${trimmed}" in
            CMD\ *|cmd\ *)
                cmd_line="${trimmed:4}"; cmd_line="${cmd_line#"${cmd_line%%[![:space:]]*}"}" ;;
            ENTRYPOINT\ *|entrypoint\ *)
                ep_line="${trimmed:11}"; ep_line="${ep_line#"${ep_line%%[![:space:]]*}"}" ;;
        esac
    done < "$DOCKERFILE"

    if [[ -n "$cmd_line" ]]; then
        local cmd; cmd=$(parse_instruction "$cmd_line")
        if [[ -n "$ep_line" ]]; then
            local ep; ep=$(parse_instruction "$ep_line")
            echo "${ep} ${cmd}"
        else
            echo "$cmd"
        fi
    elif [[ -n "$ep_line" ]]; then
        parse_instruction "$ep_line"
    else
        die "No CMD or ENTRYPOINT found in $DOCKERFILE"
    fi
}

# ── Resolve yarn/npm/pnpm script aliases ─────────────────────────

resolve_script() {
    local cmd="$1" script_name=""

    if [[ "$cmd" =~ ^(yarn|pnpm)[[:space:]]+([a-zA-Z][^[:space:]]*) ]]; then
        script_name="${BASH_REMATCH[2]}"
    elif [[ "$cmd" =~ ^(npm|pnpm)[[:space:]]+run[[:space:]]+([^[:space:]]+) ]]; then
        script_name="${BASH_REMATCH[2]}"
    elif [[ "$cmd" =~ ^npm[[:space:]]+test ]]; then
        script_name="test"
    fi

    if [[ -z "$script_name" ]]; then echo "$cmd"; return; fi
    if [[ ! -f "package.json" ]]; then echo "$cmd"; return; fi

    local resolved=""
    if command -v node &>/dev/null; then
        resolved=$(node -e "
            try { console.log(require('./package.json').scripts['$script_name'] || '') }
            catch(e) { console.log('') }
        " 2>/dev/null) || true
    elif command -v python3 &>/dev/null; then
        resolved=$(python3 -c "
import json
print(json.load(open('package.json')).get('scripts',{}).get('$script_name',''))
" 2>/dev/null) || true
    fi

    [[ -n "$resolved" ]] && echo "$resolved" || echo "$cmd"
}

# ── Detect the underlying test runner ─────────────────────────────

detect_runner() {
    local cmd="$1"
    case "$cmd" in
        pytest*|*" pytest "*|python*-m*pytest*)   echo "pytest" ;;
        *vitest*)                                  echo "vitest" ;;
        *jest*)                                    echo "jest" ;;
        *mocha*)                                   echo "mocha" ;;
        *go\ test*|go\ test*)                     echo "go" ;;
        cargo\ test*|cargo\ nextest*)              echo "cargo" ;;
        mvn\ *|./mvnw\ *)                         echo "maven" ;;
        *gradle*test*|*gradlew*test*)              echo "gradle" ;;
        *rspec*)                                   echo "rspec" ;;
        *phpunit*)                                 echo "phpunit" ;;
        dotnet\ test*)                             echo "dotnet" ;;
        mix\ test*)                                echo "mix" ;;
        swift\ test*)                              echo "swift" ;;
        sbt*test*)                                 echo "sbt" ;;
        *nose2*)                                   echo "nose2" ;;
        ruby*-I*|ruby*test*|rake\ test*)           echo "ruby" ;;
        *ctest*|*" ctest "*)                       echo "ctest" ;;
        meson\ test*|*"meson test"*)               echo "meson" ;;
        *)                                         echo "unknown" ;;
    esac
}

# ── Strip coverage flags (prevent coverage gates from failing eval) ──

strip_coverage_flags() {
    local runner="$1" cmd="$2"
    case "$runner" in
        pytest)   cmd=$(echo "$cmd" | sed -E 's/ --cov[=-][^ ]*//g; s/ --cov\b//g; s/ --no-cov-on-fail//g') ;;
        jest|vitest) cmd=$(echo "$cmd" | sed -E 's/ --coverage[^ ]*//g') ;;
        go)       cmd=$(echo "$cmd" | sed -E 's/ -cover\b//g; s/ -coverprofile[= ][^ ]*//g; s/ -coverpkg[= ][^ ]*//g') ;;
        phpunit)  cmd=$(echo "$cmd" | sed -E 's/ --coverage-[^ ]*//g') ;;
        mix)      cmd=$(echo "$cmd" | sed -E 's/ --cover\b//g') ;;
        dotnet)   cmd=$(echo "$cmd" | sed -E 's/ --collect:[^ ]*//g; s| /p:CollectCoverage=[^ ]*||g; s| /p:Threshold=[^ ]*||g') ;;
    esac
    echo "$cmd"
}

# ── JVM: convert file path to fully-qualified class name ──────────

path_to_classname() {
    local f="$1"
    for prefix in src/test/java/ src/test/kotlin/ src/test/scala/ \
                  src/main/java/ src/main/kotlin/ src/main/scala/ \
                  src/it/java/ src/it/kotlin/ src/it/scala/; do
        f="${f#$prefix}"
    done
    for ext in .java .kt .scala .groovy .kts; do
        f="${f%$ext}"
    done
    echo "${f//\//.}"
}

# ── Construct the test command for a specific set of files ────────

build_targeted_cmd() {
    local runner="$1" full_cmd="$2"
    shift 2
    local files=("$@")

    case "$runner" in
        pytest)   echo "pytest ${files[*]}" ;;
        vitest)   echo "vitest run ${files[*]}" ;;
        jest)     echo "npx jest --watchAll=false ${files[*]}" ;;
        mocha)    echo "npx mocha ${files[*]}" ;;
        nose2)    echo "nose2 ${files[*]}" ;;
        mix)      echo "mix test ${files[*]}" ;;
        ruby)     echo "ruby ${files[*]}" ;;
        rspec)
            if command -v bundle &>/dev/null; then
                echo "bundle exec rspec ${files[*]}"
            else
                echo "rspec ${files[*]}"
            fi
            ;;
        phpunit)
            if [[ -f "./vendor/bin/phpunit" ]]; then
                echo "./vendor/bin/phpunit ${files[*]}"
            else
                echo "phpunit ${files[*]}"
            fi
            ;;
        go)
            declare -A _go_dirs=()
            for f in "${files[@]}"; do
                _go_dirs["./$(dirname "$f")"]=1
            done
            # -timeout: Go packages all *_test.go in a dir together; targeting one file still runs the whole package.
            echo "go test -timeout 20m ${!_go_dirs[*]}"
            ;;
        cargo)
            local is_workspace=0
            if [[ -f "Cargo.toml" ]] && grep -q '^\[workspace\]' "Cargo.toml" 2>/dev/null; then
                is_workspace=1
            fi

            # Preserve the base command with all its flags (--features, --release, etc.)
            # by stripping only the trailing "-- <args>" portion.
            local base_cmd
            base_cmd="$(echo "$full_cmd" | sed 's/[[:space:]]\+--[[:space:]]\+.*$//')"

            declare -A _cargo_seen=()
            local cmds=()
            for f in "${files[@]}"; do
                local pkg="" pkg_dir=""
                if (( is_workspace )); then
                    local d
                    d="$(dirname "$f")"
                    while [[ -n "$d" && "$d" != "." ]]; do
                        if [[ -f "$d/Cargo.toml" ]] && grep -q '^\[package\]' "$d/Cargo.toml" 2>/dev/null; then
                            pkg=$(sed -n '/^\[package\]/,/^\[/{s/^name[[:space:]]*=[[:space:]]*"\([^"]*\)"/\1/p;}' "$d/Cargo.toml" 2>/dev/null | head -1)
                            pkg_dir="$d"
                            break
                        fi
                        d="$(dirname "$d")"
                    done
                fi
                local rel="$f"
                [[ -n "$pkg_dir" ]] && rel="${f#$pkg_dir/}"
                local flag=""
                if [[ "$rel" == tests/* ]]; then
                    flag="--test $(basename "$f" .rs)"
                else
                    flag="--lib"
                fi
                local key="${pkg:-__root__}::${flag}"
                if [[ -z "${_cargo_seen[$key]:-}" ]]; then
                    _cargo_seen["$key"]=1
                    local pfx=""
                    [[ -n "$pkg" ]] && pfx="-p $pkg "
                    cmds+=("${base_cmd} ${pfx}${flag}")
                fi
            done
            local result=""
            for ((i=0; i<${#cmds[@]}; i++)); do
                (( i > 0 )) && result+=" && "
                result+="${cmds[$i]}"
            done
            echo "$result"
            ;;
        maven)
            local classes=()
            for f in "${files[@]}"; do classes+=("$(path_to_classname "$f")"); done
            local joined; joined="$(IFS=,; echo "${classes[*]}")"
            [[ -f "./mvnw" ]] && echo "./mvnw test -Dtest=${joined}" || echo "mvn test -Dtest=${joined}"
            ;;
        gradle)
            local args=""
            for f in "${files[@]}"; do args+=" --tests $(path_to_classname "$f")"; done
            [[ -f "./gradlew" ]] && echo "./gradlew test${args}" || echo "gradle test${args}"
            ;;
        dotnet)
            local parts=()
            for f in "${files[@]}"; do parts+=("FullyQualifiedName~$(basename "$f" .cs)"); done
            local filter; filter="$(IFS='|'; echo "${parts[*]}")"
            echo "dotnet test --filter \"${filter}\""
            ;;
        swift)
            local names=()
            for f in "${files[@]}"; do names+=("$(basename "$f" .swift)"); done
            local joined; joined="$(IFS=,; echo "${names[*]}")"
            echo "swift test --filter ${joined}"
            ;;
        sbt)
            local names=()
            for f in "${files[@]}"; do names+=("$(path_to_classname "$f")"); done
            echo "sbt 'testOnly ${names[*]}'"
            ;;
        ctest)
            # Subset by CTest regex (-R): join basenames (strip common source extensions).
            local rx="" b f
            for f in "${files[@]}"; do
                b=$(basename "$f")
                b="${b%.cpp}"; b="${b%.cc}"; b="${b%.cxx}"; b="${b%.c}"; b="${b%.hpp}"
                [[ -n "$rx" ]] && rx+="|"
                rx+="$b"
            done
            if [[ "$full_cmd" =~ ^[[:space:]]*ctest(.*)$ ]]; then
                echo "ctest -R $(printf '%q' "$rx")${BASH_REMATCH[1]}"
            else
                echo "ctest -R $(printf '%q' "$rx") --test-dir build --output-on-failure"
            fi
            ;;
        meson)
            # Best-effort: meson test accepts test names as positional args (project-defined).
            local names=()
            for f in "${files[@]}"; do names+=("$(basename "$f" .cpp)"); done
            echo "meson test -C builddir --print-errorlogs ${names[*]}"
            ;;
        *)
            info "Warning: unknown runner — appending files to original command"
            echo "${full_cmd} ${files[*]}"
            ;;
    esac
}

# ── Main ──────────────────────────────────────────────────────────

[[ ! -f "$DOCKERFILE" ]] && die "$DOCKERFILE not found in $(pwd)"

FULL_CMD=$(extract_cmd)
RESOLVED=$(resolve_script "$FULL_CMD")
RUNNER=$(detect_runner "$RESOLVED")

if [[ $# -eq 0 || -z "${1:-}" ]]; then
    FULL_CMD=$(strip_coverage_flags "$RUNNER" "$FULL_CMD")
    info "=== Full test suite ==="
    info "Command: $FULL_CMD"
    info "========================"
    eval "$FULL_CMD"
else
    IFS=',' read -ra TEST_FILES <<< "$1"

    TARGETED=$(build_targeted_cmd "$RUNNER" "$FULL_CMD" "${TEST_FILES[@]}")
    TARGETED=$(strip_coverage_flags "$RUNNER" "$TARGETED")

    info "=== Targeted tests ==="
    info "Runner:  $RUNNER"
    info "Files:   ${TEST_FILES[*]}"
    info "Command: $TARGETED"
    info "========================"
    eval "$TARGETED"
fi
