#compdef kdiag
# zsh completion for kdiag
#
# Place this file as `_kdiag` somewhere on $fpath, or pipe
# `kdiag completion zsh > ${fpath[1]}/_kdiag` and start a fresh shell.
# Names of namespaces, pods, deploys, etc. are queried live from the
# cluster via `kdiag __complete`.

# Extract the namespace from the full command line. Reads $_kdiag_all_words
# (a snapshot taken at _kdiag entry) rather than $words, because nested
# _arguments handlers rebase $words to the args after the last -C boundary.
_kdiag_extract_ns() {
    local i
    for ((i=2; i<=$#_kdiag_all_words; i++)); do
        case "${_kdiag_all_words[i]}" in
            -n|--namespace)
                if (( i+1 <= $#_kdiag_all_words )); then
                    printf '%s' "${_kdiag_all_words[i+1]}"
                    return
                fi
                ;;
            --namespace=*)
                printf '%s' "${_kdiag_all_words[i]#--namespace=}"
                return
                ;;
        esac
    done
}

# Returns 0 (true) if the cursor sits right after a flag that takes a value
# (like `-n` or `--namespace`). Reads $_kdiag_all_words/$_kdiag_all_current.
# Used to keep state-branch handlers from double-adding candidates while
# the outer _arguments is handling flag-value completion.
_kdiag_at_flag_value() {
    local prev_idx=$((_kdiag_all_current - 1))
    (( prev_idx >= 1 )) || return 1
    case "${_kdiag_all_words[prev_idx]}" in
        -n|--namespace|-l|--label|--yml-path) return 0 ;;
    esac
    return 1
}

# Find the kind (pod/deploy/...) the user typed as the first positional arg
# to a given subcommand ($1 = "inspect"/"diff"). Skips flags and their values.
# Reads $_kdiag_all_words. Returns "" if not found.
_kdiag_find_kind_for() {
    local subcmd="$1"
    local i found=0
    for ((i=2; i<=$#_kdiag_all_words; i++)); do
        if (( ! found )); then
            [[ "${_kdiag_all_words[i]}" == "$subcmd" ]] && found=1
            continue
        fi
        case "${_kdiag_all_words[i]}" in
            -n|--namespace|-l|--label|--yml-path)
                # Flag that takes a value — skip both flag and value.
                ((i++))
                ;;
            -*)
                # Boolean flag or --flag=value — skip just this token.
                ;;
            *)
                # Don't treat the cursor word as a confirmed kind — the user
                # is still typing it. Returning empty lets the caller offer
                # kind completion instead of resource-name completion.
                (( i == _kdiag_all_current )) && return
                printf '%s' "${_kdiag_all_words[i]}"
                return
                ;;
        esac
    done
}

# Returns 0 (true) if the command line contains -l or --label or --label=
_kdiag_has_label() {
    local i
    for ((i=2; i<_kdiag_all_current; i++)); do
        case "${_kdiag_all_words[i]}" in
            -l|--label|--label=*)
                return 0
                ;;
        esac
    done
    return 1
}

# Count positional arguments after the kind for a subcommand.
# Reads $_kdiag_all_words/$_kdiag_all_current.
_kdiag_count_positionals() {
    local subcmd="$1"
    local i found_subcmd=0 found_kind=0 count=0
    for ((i=2; i<=$#_kdiag_all_words; i++)); do
        if (( ! found_subcmd )); then
            if [[ "${_kdiag_all_words[i]}" == "$subcmd" ]]; then
                found_subcmd=1
            fi
            continue
        fi
        if (( ! found_kind )); then
            case "${_kdiag_all_words[i]}" in
                -n|--namespace|-l|--label|--yml-path)
                    ((i++))
                    ;;
                -*)
                    ;;
                *)
                    # Found the kind. Do not count the current word as kind if we are typing it.
                    if (( i == _kdiag_all_current )); then
                        return
                    fi
                    found_kind=1
                    ;;
            esac
            continue
        fi
        # We have found both the subcommand and the kind. Now we count positionals after the kind.
        case "${_kdiag_all_words[i]}" in
            -n|--namespace|-l|--label|--yml-path)
                ((i++))
                ;;
            -*)
                ;;
            *)
                # Don't count the word currently being typed (at cursor index).
                if (( i != _kdiag_all_current )); then
                    ((count++))
                fi
                ;;
        esac
    done
    printf '%d' "$count"
}

_kdiag_namespaces() {
    local -a ns
    ns=( ${(f)"$(kdiag __complete namespaces 2>/dev/null)"} )
    (( ${#ns} )) && compadd -a ns
}

# $1 = kind (pod/deploy/...). Reads namespace from the saved command line.
_kdiag_resource_names() {
    # When completing a flag (current word starts with `-`), don't query
    # the cluster — keeps flag completion fast and prevents resource names
    # from leaking into the flag list on zsh 5.9+.
    [[ "$PREFIX" == -* ]] && return
    local kind="$1"
    local ns
    ns="$(_kdiag_extract_ns)"
    local -a names
    names=( ${(f)"$(kdiag __complete resources $kind $ns 2>/dev/null)"} )
    (( ${#names} )) && compadd -a names
}

_kdiag() {
    # Snapshot of the full command line; read by _kdiag_extract_ns from
    # within nested _arguments states where $words has been rebased.
    local -a _kdiag_all_words=("${words[@]}")
    local _kdiag_all_current=$CURRENT

    # Suppress the "option" header zsh prints above the flag list for kdiag
    # — scoped to kdiag contexts only so user's global zstyle is untouched.
    zstyle ':completion::*:kdiag:*' format ''
    zstyle ':completion::*:kdiag-*:*' format ''

    # View-selector detection: mirrors the bash script's view_seen logic so
    # tab completion hides flags that don't compose with whatever view the
    # user has already typed.
    local view_seen=""
    local _i
    for ((_i=2; _i<=$#_kdiag_all_words; _i++)); do
        case "${_kdiag_all_words[_i]}" in
            --yml-path)   view_seen=ymlpath ;;
            --resources)  view_seen=resources ;;
            --spec)       view_seen=spec ;;
            --az)         view_seen=az ;;
        esac
    done

    local -a top_cmds inspect_kinds diff_kinds completion_shells
    top_cmds=(
        'inspect:Show container/workload state for pods, deploy, ds, sts, rs, node'
        'diff:Diff Kubernetes resources (rs, pod, node)'
        'events:Show events in the current namespace'
        'sort:Sort resources by creation date (newest last)'
    )
    inspect_kinds=(pod deployment daemonset statefulset replicaset node)
    # sort accepts any kind the API server exposes (built-in or CRD); these
    # are just suggestions for tab completion.
    sort_kinds=(pod deployment daemonset statefulset replicaset node namespace configmap secret service ingress endpoints endpointslice persistentvolumeclaim persistentvolume storageclass serviceaccount role rolebinding clusterrole clusterrolebinding horizontalpodautoscaler poddisruptionbudget limitrange resourcequota job cronjob networkpolicy priorityclass runtimeclass lease customresourcedefinition)
    # diff accepts any kind the API server exposes (built-in or CRD); these
    # are just suggestions for tab completion. Users can type any kind they
    # like — it resolves at runtime via the cluster's discovery doc.
    diff_kinds=($sort_kinds)
    completion_shells=(bash zsh)

    # Note: the value-spec labels are kept as a single space (": :action") so
    # zsh doesn't render a category header above the candidates. Users want
    # clean kubectl-style lists, not a "namespace"/"file" header.
    local -a shared_flags inspect_flags events_flags sort_flags
    shared_flags=(
        '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
        '(-l --label)'{-l,--label}'[Label selector]: :'
    )
    local -a inspect_flags
    case "${view_seen}" in
        ymlpath)
            inspect_flags=(
                $shared_flags
                '--yml-path[Walk YAML and print yq paths matching a key or value]'
            )
            ;;
        resources)
            inspect_flags=(
                $shared_flags
                '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                '--yaml[Emit yq-safe YAML instead of text]'
                '--az[Show availability-zone placement]'
            )
            ;;
        spec)
            inspect_flags=(
                $shared_flags
                '--spec[deploy: print .spec.template.spec as YAML]'
                '--yaml[Emit yq-safe YAML instead of text]'
            )
            ;;
        az)
            inspect_flags=(
                $shared_flags
                '--az[Show availability-zone placement]'
                '--yaml[Emit yq-safe YAML instead of text]'
            )
            ;;
        *)
            inspect_flags=(
                $shared_flags
                '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                '--yaml[Emit yq-safe YAML instead of text]'
                '--yml-path[Walk YAML and print yq paths matching a key or value]'
                '--az[Show availability-zone placement]'
                '--spec[deploy: print .spec.template.spec as YAML]'
            )
            ;;
    esac
    events_flags=(
        '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
        '(-A --all-namespaces)'{-A,--all-namespaces}'[List events across all namespaces]'
        '--since[Only show events newer than this duration, e.g. 30s, 5m, 2h]: :'
    )
    sort_flags=(
        '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
        '(-A --all-namespaces)'{-A,--all-namespaces}'[List resources across all namespaces]'
    )

    local context state line
    _arguments -C \
        '1: :->cmd' \
        '*::arg:->args'

    case $state in
        cmd)
            _describe 'command' top_cmds
            ;;
        args)
            case $line[1] in
                inspect)
                    # Dispatch by intent. We avoid `*::arg:->state` because
                    # nested-state rebasing makes `"1: :{action}"` unreliable
                    # and also tends to double-add flag candidates.
                    local kind="$(_kdiag_find_kind_for inspect)"
                    local canonical
                    case "$kind" in
                        po|pod)            canonical=pod ;;
                        deploy|deployment) canonical=deployment ;;
                        ds|daemonset)      canonical=daemonset ;;
                        sts|statefulset)   canonical=statefulset ;;
                        rs|replicaset)     canonical=replicaset ;;
                        no|node)           canonical=node ;;
                        *)                 canonical="" ;;
                    esac

                    if [[ -n "$canonical" && "$PREFIX" != -* ]] \
                       && ! _kdiag_at_flag_value \
                       && ! _kdiag_has_label \
                       && [[ "$(_kdiag_count_positionals inspect)" -eq 0 ]]; then
                        # Cursor is at a resource-name positional.
                        _kdiag_resource_names "$canonical"
                    else
                        # Kind not chosen yet, flag prefix, or flag-value slot —
                        # let _arguments handle kinds + flags + flag values.
                        # Per-kind flags must match the Go flagset registered in
                        # internal/cmd/inspect_{pod,deploy,workloads,node}.go.
                        local -a kflags
                        case "$canonical" in
                            pod)
                                case "${view_seen}" in
                                    ymlpath)
                                        kflags=(
                                            $shared_flags
                                            '--yml-path[Walk YAML and print yq paths matching a key or value]'
                                        )
                                        ;;
                                    resources)
                                        kflags=(
                                            $shared_flags
                                            '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                            '--az[Show availability-zone placement]'
                                        )
                                        ;;
                                    az)
                                        kflags=(
                                            $shared_flags
                                            '--az[Show availability-zone placement]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                        )
                                        ;;
                                    *)
                                        kflags=(
                                            $shared_flags
                                            '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                            '--yml-path[Walk YAML and print yq paths matching a key or value]'
                                            '--az[Show availability-zone placement]'
                                        )
                                        ;;
                                esac
                                ;;
                            deployment)
                                case "${view_seen}" in
                                    ymlpath)
                                        kflags=(
                                            $shared_flags
                                            '--yml-path[Walk YAML and print yq paths matching a key or value]'
                                        )
                                        ;;
                                    resources)
                                        kflags=(
                                            $shared_flags
                                            '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                            '--az[Show availability-zone placement]'
                                        )
                                        ;;
                                    spec)
                                        kflags=(
                                            $shared_flags
                                            '--spec[deploy: print .spec.template.spec as YAML]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                        )
                                        ;;
                                    az)
                                        kflags=(
                                            $shared_flags
                                            '--az[Show availability-zone placement]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                        )
                                        ;;
                                    *)
                                        kflags=(
                                            $shared_flags
                                            '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                            '--yml-path[Walk YAML and print yq paths matching a key or value]'
                                            '--az[Show availability-zone placement]'
                                            '--spec[deploy: print .spec.template.spec as YAML]'
                                        )
                                        ;;
                                esac
                                ;;
                            daemonset|statefulset|replicaset)
                                case "${view_seen}" in
                                    ymlpath)
                                        kflags=(
                                            '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
                                            '--yml-path[Walk YAML and print yq paths matching a key or value]'
                                        )
                                        ;;
                                    resources)
                                        kflags=(
                                            '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
                                            '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                            '--az[Show availability-zone placement]'
                                        )
                                        ;;
                                    az)
                                        kflags=(
                                            '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
                                            '--az[Show availability-zone placement]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                        )
                                        ;;
                                    *)
                                        kflags=(
                                            '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
                                            '--resources[Show resource requests/limits as YAML (pod/deploy)]'
                                            '--yaml[Emit yq-safe YAML instead of text]'
                                            '--yml-path[Walk YAML and print yq paths matching a key or value]'
                                            '--az[Show availability-zone placement]'
                                        )
                                        ;;
                                esac
                                ;;
                            node)
                                kflags=($shared_flags)
                                ;;
                            *)
                                # Kind not yet typed — offer the union so a
                                # flag before a kind can still complete.
                                kflags=($inspect_flags)
                                ;;
                        esac
                        _arguments -C \
                            "1: :(${inspect_kinds[*]})" \
                            $kflags
                    fi
                    ;;
                diff)
                    local dkind="$(_kdiag_find_kind_for diff)"
                    # diff rs revision-diff mode: the positional is the
                    # *deployment* name, not the RS name. Everything else
                    # uses the typed kind directly — the Go side resolves
                    # aliases via the discovery doc.
                    local target_kind=""
                    case "$dkind" in
                        "")            target_kind="" ;;
                        rs|replicaset) target_kind="deployment" ;;
                        *)             target_kind="$dkind" ;;
                    esac

                    local show_res=0
                    if [[ -n "$target_kind" && "$PREFIX" != -* ]] \
                       && ! _kdiag_at_flag_value; then
                        local pos_count="$(_kdiag_count_positionals diff)"
                        case "$dkind" in
                            rs|replicaset)
                                if ! _kdiag_has_label && [[ "$pos_count" -eq 0 ]]; then
                                    show_res=1
                                fi
                                ;;
                            *)
                                if [[ "$pos_count" -lt 2 ]]; then
                                    show_res=1
                                fi
                                ;;
                        esac
                    fi

                    if (( show_res )); then
                        _kdiag_resource_names "$target_kind"
                    else
                        # Per-kind flags must match the Go flagset registered in
                        # internal/cmd/diff.go (runDiffGeneric / runDiffReplicaSet).
                        local -a dflags
                        case "$dkind" in
                            rs|replicaset)
                                dflags=(
                                    $shared_flags
                                    '--full[Show raw API response; for rs, dump full RS objects instead of just .spec.template]'
                                )
                                ;;
                            "")
                                # Kind not typed yet — offer the union so a
                                # flag typed before a kind can still complete.
                                dflags=(
                                    $shared_flags
                                    '--full[Show raw API response without per-kind noise stripping; for rs, dump full RS objects]'
                                )
                                ;;
                            *)
                                dflags=(
                                    '(-n --namespace)'{-n,--namespace}'[Namespace]: :_kdiag_namespaces'
                                    '--full[Show raw API response without per-kind noise stripping]'
                                )
                                ;;
                        esac
                        _arguments -C \
                            "1: :(${diff_kinds[*]})" \
                            $dflags
                    fi
                    ;;
                events)
                    _arguments $events_flags
                    ;;
                sort)
                    # zsh `_arguments` quirk: with exactly two long flags
                    # where one ("--namespace") is a substring of the other
                    # ("--all-namespaces"), typing `--<TAB>` auto-inserts
                    # "--namespace" instead of listing both candidates.
                    # `events` is unaffected because its third flag (`--since`)
                    # breaks the two-candidate degeneracy. Detour to
                    # `_describe` only for the exact `--` prefix; everything
                    # else (including `--n`/`--a` prefixes, flag values, and
                    # the kind positional) still flows through `_arguments`.
                    if [[ "$PREFIX" == "--" ]] && ! _kdiag_at_flag_value; then
                        local -a sort_long_flags
                        sort_long_flags=(
                            '--namespace:Namespace'
                            '--all-namespaces:List resources across all namespaces (overrides -n)'
                        )
                        _describe -t options 'option' sort_long_flags
                    else
                        _arguments -C \
                            "1: :(${sort_kinds[*]})" \
                            $sort_flags
                    fi
                    ;;
                completion)
                    _arguments "1: :(${completion_shells[*]})"
                    ;;
            esac
            ;;
    esac
}

_kdiag "$@"
