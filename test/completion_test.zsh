#!/usr/bin/env zsh
#
# Zsh completion test suite for kdiag.
#
# One test function per main command. Each test drives an interactive zsh via
# zpty, triggers tab completion, and asserts:
#   - the expected kinds/subcommands are offered,
#   - every flag the Go flagset accepts is offered,
#   - no flag absent from the Go flagset is offered (zsh<->Go drift),
#   - no `_arguments`/`comparguments` parser errors are emitted.
#
# Run via: make completion-tests
# Or directly: zsh test/completion_test.zsh

set -u
emulate -L zsh
zmodload zsh/zpty

SCRIPT_DIR="${0:A:h}"
REPO_DIR="${SCRIPT_DIR}/.."
KDIAG="${REPO_DIR}/kdiag"
COMP_FILE="${TMPDIR:-/tmp}/_kdiag"

PASS=0
FAIL=0
typeset -a FAILURES

ansi_strip() { sed -E 's/\x1B\[[0-9;?]*[a-zA-Z]//g; s/\x1B\][^\x07]*\x07//g; s/\r//g' }

log_pass()   { printf '  \033[32m✓\033[0m %s\n' "$1"; PASS=$((PASS+1)); }
log_fail()   {
    local label="$1" detail="$2"
    printf '  \033[31m✗\033[0m %s\n' "$label"
    printf '      %s\n' "$detail"
    FAIL=$((FAIL+1))
    FAILURES+=("${label}|${detail}")
}
log_skip()   { printf '  \033[33m⊘\033[0m %s (%s)\n' "$1" "$2"; }
section()    { printf '\n\033[1m== %s ==\033[0m\n' "$1"; }

build_if_needed() {
    if [[ ! -x "$KDIAG" ]] || [[ -n "$(find "$REPO_DIR" -name '*.go' -newer "$KDIAG" -print -quit 2>/dev/null)" ]]; then
        printf 'building kdiag...\n'
        (cd "$REPO_DIR" && make build >/dev/null) || { print -u2 'build failed'; exit 2 }
    fi
    "$KDIAG" completion zsh > "$COMP_FILE" || { print -u2 'completion generation failed'; exit 2 }
}

# wait_prompt — read pty output until our sentinel prompt appears or timeout
wait_prompt() {
    local marker='READY>' acc='' chunk
    local deadline=$(( SECONDS + 5 ))
    while (( SECONDS < deadline )); do
        chunk=''
        zpty -r -t shell chunk 0.1 >/dev/null 2>&1 || true
        acc+="$chunk"
        [[ "$acc" == *"$marker"* ]] && return 0
    done
    return 1
}

# drive <cmdline>
# Types $1 then TAB into a fresh `zsh -f -i`, captures the screen contents,
# normalizes ANSI/cursor noise, and emits the result. A canned terminal width
# (COLUMNS=200) keeps menu items from wrapping into the next line and ensures
# inline-menu items stay newline-separated.
drive() {
    local cmdline="$1"
    local out=''
    local comp_dir="${COMP_FILE:h}"
    zpty -b shell zsh -f -i
    zpty -r -t shell _ 0.3 >/dev/null 2>&1 || true
    zpty -w shell "export COLUMNS=200 LINES=80; unset PROMPT_EOL_MARK PROMPT_SP; PS1='READY> '"
    wait_prompt
    zpty -w shell "fpath=($comp_dir \$fpath); autoload -Uz compinit; compinit -u; PATH=$REPO_DIR:\$PATH"
    wait_prompt
    # `setopt LIST_PACKED` off so each candidate ends up on its own line in
    # the menu, regardless of width. ALWAYS_LAST_PROMPT off avoids prompt
    # redraw on the same line as the last menu item.
    zpty -w shell "unsetopt ALWAYS_LAST_PROMPT 2>/dev/null; zstyle ':completion:*' list-packed false; zstyle ':completion:*' list-grouped false"
    wait_prompt
    zpty -w -n shell "${cmdline}"$'\t'
    sleep 0.9
    zpty -r -t shell out 0.5 || true
    zpty -d shell 2>/dev/null || true
    # Normalize: strip ANSI/OSC, drop CR, then collapse any sequence of
    # whitespace at the end of a line so trailing escape junk doesn't fuse
    # the last menu item with the prompt redraw on the next line.
    print -r -- "$out" | ansi_strip | sed -E 's/[[:space:]]+$//'
}

# A candidate (kind or flag) is "offered" if it appears as a whole token in
# the captured menu output. Token boundaries: start-of-line, end-of-line, or
# any character that isn't part of a candidate identifier (so `--label` won't
# match inside `--all-namespaces`, and `pod` won't match inside `pods`).
flag_offered() {
    local flag="$1" output="$2"
    # For flags, allow trailing junk (terminal escape residue) IF preceded by
    # whitespace/start-of-line — flag names are unambiguous.
    print -r -- "$output" | grep -qE "(^|[[:space:]])${flag}([^a-zA-Z0-9_-]|$)"
}

kind_offered() {
    local kind="$1" output="$2"
    # Match kind preceded by space/start and followed by space/end OR the
    # standard end-of-menu boundary characters that terminal redraw inserts
    # (anything other than [a-zA-Z0-9_-]).
    print -r -- "$output" | grep -qE "(^|[[:space:]])${kind}([^a-zA-Z0-9_-]|$)"
}

parser_error_line() {
    print -r -- "$1" | grep -E '_arguments:|comparguments:|bad option|invalid option' | head -1
}

assert_flags_present() {
    local label="$1" output="$2"; shift 2
    local flag
    for flag in "$@"; do
        if flag_offered "$flag" "$output"; then
            log_pass "$label: $flag offered"
        else
            log_fail "$label: $flag NOT offered" "Go flagset accepts $flag but zsh did not list it."
        fi
    done
}

assert_flags_absent() {
    local label="$1" output="$2"; shift 2
    local flag
    for flag in "$@"; do
        if flag_offered "$flag" "$output"; then
            log_fail "$label: $flag offered but NOT in Go flagset" \
                     "Drift: zsh lists $flag for a context where the Go code rejects it."
        else
            log_pass "$label: $flag correctly absent"
        fi
    done
}

assert_no_parser_error() {
    local label="$1" output="$2"
    local err=$(parser_error_line "$output")
    if [[ -n "$err" ]]; then
        log_fail "$label: parser error" "$err"
    else
        log_pass "$label: no parser error"
    fi
}

assert_offered() {
    local label="$1" needle="$2" output="$3"
    if kind_offered "$needle" "$output"; then
        log_pass "$label: '$needle' offered"
    else
        log_fail "$label: '$needle' NOT offered" "Expected '$needle' in the completion menu."
    fi
}

# ── inspect ──────────────────────────────────────────────────────────────────

# Source of truth derived from internal/cmd/inspect_*.go.
typeset -gA INSPECT_FLAGS
INSPECT_FLAGS[pod]="--namespace --resources --label --az --container-spec"
INSPECT_FLAGS[deployment]="--namespace --resources --label --az --spec --container-spec"
INSPECT_FLAGS[daemonset]="--namespace --resources --az"
INSPECT_FLAGS[statefulset]="--namespace --resources --az"
INSPECT_FLAGS[replicaset]="--namespace --resources --az"
INSPECT_FLAGS[node]="--namespace --label"

ALL_INSPECT_FLAGS=(--namespace --resources --label --az --spec --container-spec)
INSPECT_KINDS=(pod deployment daemonset statefulset replicaset node)

test_inspect() {
    section "inspect"

    local out=$(drive 'kdiag inspect ')
    assert_no_parser_error "inspect <TAB>" "$out"
    local kind
    for kind in $INSPECT_KINDS; do
        assert_offered "inspect <TAB>" "$kind" "$out"
    done

    for kind in $INSPECT_KINDS; do
        out=$(drive "kdiag inspect $kind --")
        local label="inspect $kind --<TAB>"
        assert_no_parser_error "$label" "$out"
        local expected=(${(s: :)INSPECT_FLAGS[$kind]})
        assert_flags_present "$label" "$out" $expected
        # Flags from the union that are NOT expected for this kind must be absent.
        local extra=()
        local f
        for f in $ALL_INSPECT_FLAGS; do
            if (( ! ${expected[(I)$f]} )); then
                extra+=("$f")
            fi
        done
        if (( ${#extra} )); then
            assert_flags_absent "$label" "$out" $extra
        fi
    done
}

# ── diff ─────────────────────────────────────────────────────────────────────

typeset -gA DIFF_FLAGS
DIFF_FLAGS[replicaset]="--namespace --label --full"
DIFF_FLAGS[pod]="--namespace --full"
DIFF_FLAGS[node]="--namespace --full"
DIFF_FLAGS[deployment]="--namespace --full"
DIFF_FLAGS[configmap]="--namespace --full"
DIFF_FLAGS[service]="--namespace --full"

ALL_DIFF_FLAGS=(--namespace --label --full)
DIFF_KINDS=(replicaset pod node deployment configmap service)

# Kinds we want to see in `kdiag diff <TAB>` suggestions. diff accepts any
# kind the API server exposes (including CRDs); this list just spot-checks
# that the completion script offers a sensible set of common built-ins.
DIFF_OFFERED_KINDS=(pod deployment daemonset statefulset replicaset node
    configmap secret service ingress persistentvolumeclaim persistentvolume
    serviceaccount job cronjob)

test_diff() {
    section "diff"

    local out=$(drive 'kdiag diff ')
    assert_no_parser_error "diff <TAB>" "$out"
    local kind
    for kind in $DIFF_OFFERED_KINDS; do
        assert_offered "diff <TAB>" "$kind" "$out"
    done

    for kind in $DIFF_KINDS; do
        out=$(drive "kdiag diff $kind --")
        local label="diff $kind --<TAB>"
        assert_no_parser_error "$label" "$out"
        local expected=(${(s: :)DIFF_FLAGS[$kind]})
        (( ${#expected} )) && assert_flags_present "$label" "$out" $expected
        local extra=()
        local f
        for f in $ALL_DIFF_FLAGS; do
            if (( ! ${expected[(I)$f]} )); then
                extra+=("$f")
            fi
        done
        (( ${#extra} )) && assert_flags_absent "$label" "$out" $extra
    done
}

# ── events ───────────────────────────────────────────────────────────────────

test_events() {
    section "events"
    local out=$(drive 'kdiag events --')
    local label="events --<TAB>"
    assert_no_parser_error "$label" "$out"
    assert_flags_present "$label" "$out" --namespace --all-namespaces --since
    assert_flags_absent  "$label" "$out" --label --resources
}

# ── sort ─────────────────────────────────────────────────────────────────────

test_sort() {
    section "sort"
    local out=$(drive 'kdiag sort --')
    local label="sort --<TAB>"
    assert_no_parser_error "$label" "$out"
    assert_flags_present "$label" "$out" --namespace --all-namespaces
    assert_flags_absent  "$label" "$out" --label --resources --since
}

# ── runner ───────────────────────────────────────────────────────────────────

build_if_needed

test_inspect
test_diff
test_events
test_sort

printf '\n── summary ──\n'
printf 'passed: %d\n' $PASS
printf 'failed: %d\n' $FAIL
if (( FAIL > 0 )); then
    printf '\nfailures:\n'
    local entry
    for entry in $FAILURES; do
        printf '  %s\n' "${entry%%|*}"
        printf '      %s\n' "${entry#*|}"
    done
    exit 1
fi
exit 0
