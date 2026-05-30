# bash completion for kdiag
#
# Source this file or pipe `kdiag completion bash | source` from your
# .bashrc to enable tab completion. Names of namespaces, pods, deploys,
# etc. are queried live from the cluster via `kdiag __complete`.

# Walk COMP_WORDS up to (but not including) the cursor, returning the
# value passed to the most recent `-n` / `--namespace` / `--namespace=`.
# Empty if none.
_kdiag_extract_ns() {
    local i
    for ((i=1; i<COMP_CWORD; i++)); do
        case "${COMP_WORDS[i]}" in
            -n|--namespace)
                if (( i+1 < COMP_CWORD )); then
                    printf '%s' "${COMP_WORDS[i+1]}"
                    return
                fi
                ;;
            --namespace=*)
                printf '%s' "${COMP_WORDS[i]#--namespace=}"
                return
                ;;
        esac
    done
}

# Count positional arguments starting from index $1.
_kdiag_count_positionals() {
    local start_idx=$1
    local count=0
    local i
    for ((i=start_idx; i<COMP_CWORD; i++)); do
        case "${COMP_WORDS[i]}" in
            -n|--namespace|-l|--label|--path|--since)
                # Skip flag and its value
                ((i++))
                ;;
            -*)
                # Skip boolean flags
                ;;
            *)
                ((count++))
                ;;
        esac
    done
    printf '%d' "${count}"
}

# Returns 0 (true) if the command line contains -l or --label or --label=
_kdiag_has_label() {
    local i
    for ((i=1; i<COMP_CWORD; i++)); do
        case "${COMP_WORDS[i]}" in
            -l|--label|--label=*)
                return 0
                ;;
        esac
    done
    return 1
}

# Find the index of the kind positional argument under inspect/diff/troubleshoot.
_kdiag_find_kind_idx() {
    local i
    for ((i=2; i<COMP_CWORD; i++)); do
        case "${COMP_WORDS[i]}" in
            -n|--namespace|-l|--label|--path)
                ((i++))
                ;;
            -*)
                ;;
            *)
                printf '%d' "${i}"
                return
                ;;
        esac
    done
    printf '-1'
}

_kdiag() {
    local cur prev cword
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cword=${COMP_CWORD}

    # Top-level completion suggestions exclude the housekeeping commands
    # (completion, help) — they remain valid invocations, but are hidden
    # from `kdiag <TAB>` to match the bare-banner / -h split.
    local top_cmds="diff events inspect sort troubleshoot"
    local help_topics="completion diff events inspect sort troubleshoot yml-path path"
    local inspect_kinds="pod deployment daemonset statefulset replicaset node"
    # sort accepts any kind the API server exposes (built-in or CRD); these
    # are just suggestions for tab completion.
    local sort_kinds="pod deployment daemonset statefulset replicaset node namespace configmap secret service ingress endpoints endpointslice persistentvolumeclaim persistentvolume storageclass serviceaccount role rolebinding clusterrole clusterrolebinding horizontalpodautoscaler poddisruptionbudget limitrange resourcequota job cronjob networkpolicy priorityclass runtimeclass lease customresourcedefinition"
    # diff accepts any kind the API server exposes (built-in or CRD); these
    # are just suggestions for tab completion. Users can type any kind they
    # like — it resolves at runtime via the cluster's discovery doc.
    local diff_kinds="${sort_kinds}"
    local completion_shells="bash zsh"
    local ai_providers="claude gemini chatgpt"
    local shared_flags="--namespace -n --label -l"
    local events_flags="--namespace -n --all-namespaces -A --since"
    local sort_flags="--namespace -n --all-namespaces -A"
    local troubleshoot_flags="${shared_flags} --yaml --ai"

    # Flag-value completion: when the previous token expects a value.
    case "${prev}" in
        -n|--namespace)
            local nss
            nss="$(kdiag __complete namespaces "${cur}" 2>/dev/null)"
            COMPREPLY=( $(compgen -W "${nss}" -- "${cur}") )
            return
            ;;
        --label|-l|--path)
            return
            ;;
    esac

    # --ai takes an optional provider via the = form (--ai=claude).
    if [[ "${cur}" == --ai=* ]]; then
        COMPREPLY=( $(compgen -W "${ai_providers}" -P "--ai=" -- "${cur#--ai=}") )
        return
    fi

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${top_cmds}" -- "${cur}") )
        return
    fi

    local cmd="${COMP_WORDS[1]}"

    # Detect which view selector (if any) is already on the command line.
    # Once a view selector is present, only suggest flags that compose with it
    # — the others are mutually exclusive (each selects a view).
    local view_seen=""
    local _i
    for ((_i=1; _i<COMP_CWORD; _i++)); do
        case "${COMP_WORDS[_i]}" in
            --path)              view_seen=path ;;
            --resources)         view_seen=resources ;;
            --deployment-spec)   view_seen=spec ;;
            --az)                view_seen=az ;;
            --pods)              view_seen=pods ;;
        esac
    done

    local inspect_flags
    case "${view_seen}" in
        path)         inspect_flags="${shared_flags} --path" ;;
        resources)    inspect_flags="${shared_flags} --resources --az --yaml" ;;
        spec)         inspect_flags="${shared_flags} --deployment-spec --yaml" ;;
        az)           inspect_flags="${shared_flags} --az --yaml" ;;
        pods)         inspect_flags="${shared_flags} --pods --yaml" ;;
        *)            inspect_flags="${shared_flags} --resources --az --deployment-spec --pods --yaml --path" ;;
    esac

    case "${cmd}" in
        inspect)
            local kind_idx
            kind_idx="$(_kdiag_find_kind_idx)"
            if [[ ${kind_idx} -eq -1 ]]; then
                if [[ "${cur}" == -* ]]; then
                    COMPREPLY=( $(compgen -W "${inspect_flags}" -- "${cur}") )
                else
                    COMPREPLY=( $(compgen -W "${inspect_kinds}" -- "${cur}") )
                fi
                return
            fi
            local kind="${COMP_WORDS[kind_idx]}"
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=( $(compgen -W "${inspect_flags}" -- "${cur}") )
                return
            fi
            if _kdiag_has_label; then
                return
            fi
            local pos_count
            pos_count="$(_kdiag_count_positionals $((kind_idx + 1)))"
            if [[ ${pos_count} -eq 0 ]]; then
                local ns
                ns="$(_kdiag_extract_ns)"
                local names
                names="$(kdiag __complete resources "${kind}" "${ns}" "${cur}" 2>/dev/null)"
                COMPREPLY=( $(compgen -W "${names}" -- "${cur}") )
            else
                # Positional cap reached (inspect <kind> accepts one name) —
                # offer only flags so users don't tack on a second name.
                COMPREPLY=( $(compgen -W "${inspect_flags}" -- "${cur}") )
            fi
            ;;
        troubleshoot)
            # kind first (pod/deploy/.../node), then a small flag set. No view
            # selectors — troubleshoot is its own diagnostic.
            local kind_idx
            kind_idx="$(_kdiag_find_kind_idx)"
            if [[ ${kind_idx} -eq -1 ]]; then
                if [[ "${cur}" == -* ]]; then
                    COMPREPLY=( $(compgen -W "${troubleshoot_flags}" -- "${cur}") )
                else
                    COMPREPLY=( $(compgen -W "${inspect_kinds}" -- "${cur}") )
                fi
                return
            fi
            local kind="${COMP_WORDS[kind_idx]}"
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=( $(compgen -W "${troubleshoot_flags}" -- "${cur}") )
                return
            fi
            if _kdiag_has_label; then
                return
            fi
            local pos_count
            pos_count="$(_kdiag_count_positionals $((kind_idx + 1)))"
            if [[ ${pos_count} -eq 0 ]]; then
                local ns
                ns="$(_kdiag_extract_ns)"
                local names
                names="$(kdiag __complete resources "${kind}" "${ns}" "${cur}" 2>/dev/null)"
                COMPREPLY=( $(compgen -W "${names}" -- "${cur}") )
            else
                COMPREPLY=( $(compgen -W "${troubleshoot_flags}" -- "${cur}") )
            fi
            ;;
        diff)
            local kind_idx
            kind_idx="$(_kdiag_find_kind_idx)"
            if [[ ${kind_idx} -eq -1 ]]; then
                if [[ "${cur}" == -* ]]; then
                    COMPREPLY=( $(compgen -W "--namespace -n --full --label -l" -- "${cur}") )
                else
                    COMPREPLY=( $(compgen -W "${diff_kinds}" -- "${cur}") )
                fi
                return
            fi
            local diff_kind="${COMP_WORDS[kind_idx]}"
            if [[ "${cur}" == -* ]]; then
                local diff_flags
                case "${diff_kind}" in
                    rs|replicaset)
                        diff_flags="--namespace -n --label -l --full"
                        ;;
                    *)
                        diff_flags="--namespace -n --full"
                        ;;
                esac
                COMPREPLY=( $(compgen -W "${diff_flags}" -- "${cur}") )
                return
            fi
            local ns
            ns="$(_kdiag_extract_ns)"
            local target_kind="${diff_kind}"
            case "${diff_kind}" in
                rs|replicaset)
                    target_kind="deployment"
                    if _kdiag_has_label; then
                        return
                    fi
                    local pos_count
                    pos_count="$(_kdiag_count_positionals $((kind_idx + 1)))"
                    if [[ ${pos_count} -eq 0 ]]; then
                        local names
                        names="$(kdiag __complete resources "${target_kind}" "${ns}" "${cur}" 2>/dev/null)"
                        COMPREPLY=( $(compgen -W "${names}" -- "${cur}") )
                    fi
                    ;;
                *)
                    local pos_count
                    pos_count="$(_kdiag_count_positionals $((kind_idx + 1)))"
                    if [[ ${pos_count} -lt 2 ]]; then
                        local names
                        names="$(kdiag __complete resources "${target_kind}" "${ns}" "${cur}" 2>/dev/null)"
                        COMPREPLY=( $(compgen -W "${names}" -- "${cur}") )
                    fi
                    ;;
            esac
            ;;
        events)
            COMPREPLY=( $(compgen -W "${events_flags}" -- "${cur}") )
            ;;
        sort)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${sort_kinds}" -- "${cur}") )
                return
            fi
            COMPREPLY=( $(compgen -W "${sort_flags}" -- "${cur}") )
            ;;
        completion)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${completion_shells}" -- "${cur}") )
            fi
            ;;
        help)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${help_topics}" -- "${cur}") )
            fi
            ;;
    esac
}

complete -F _kdiag kdiag
