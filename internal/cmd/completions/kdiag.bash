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

_kdiag() {
    local cur prev cword
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cword=${COMP_CWORD}

    local top_cmds="inspect diff events sort"
    local inspect_kinds="pod deployment daemonset statefulset replicaset node"
    # sort accepts any kind the API server exposes (built-in or CRD); these
    # are just suggestions for tab completion.
    local sort_kinds="pod deployment daemonset statefulset replicaset node namespace configmap secret service ingress endpoints endpointslice persistentvolumeclaim persistentvolume storageclass serviceaccount role rolebinding clusterrole clusterrolebinding horizontalpodautoscaler poddisruptionbudget limitrange resourcequota job cronjob networkpolicy priorityclass runtimeclass lease customresourcedefinition"
    # diff accepts any kind the API server exposes (built-in or CRD); these
    # are just suggestions for tab completion. Users can type any kind they
    # like — it resolves at runtime via the cluster's discovery doc.
    local diff_kinds="${sort_kinds}"
    local completion_shells="bash zsh"
    local shared_flags="--namespace -n --label -l"
    local inspect_flags="${shared_flags} --resources --az --spec --container-spec"
    local events_flags="--namespace -n --all-namespaces -A --since"
    local sort_flags="--namespace -n --all-namespaces -A"

    # Flag-value completion: when the previous token expects a value.
    case "${prev}" in
        -n|--namespace)
            local nss
            nss="$(kdiag __complete namespaces "${cur}" 2>/dev/null)"
            COMPREPLY=( $(compgen -W "${nss}" -- "${cur}") )
            return
            ;;
        --label|-l)
            return
            ;;
    esac

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${top_cmds}" -- "${cur}") )
        return
    fi

    local cmd="${COMP_WORDS[1]}"
    case "${cmd}" in
        inspect)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${inspect_kinds}" -- "${cur}") )
                return
            fi
            # cword >= 3: complete flags or a resource name positional.
            if [[ "${cur}" == -* ]]; then
                COMPREPLY=( $(compgen -W "${inspect_flags}" -- "${cur}") )
                return
            fi
            local kind="${COMP_WORDS[2]}"
            local ns
            ns="$(_kdiag_extract_ns)"
            local names
            names="$(kdiag __complete resources "${kind}" "${ns}" "${cur}" 2>/dev/null)"
            COMPREPLY=( $(compgen -W "${names}" -- "${cur}") )
            ;;
        diff)
            if [[ ${cword} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "${diff_kinds}" -- "${cur}") )
                return
            fi
            local diff_kind="${COMP_WORDS[2]}"
            if [[ "${cur}" == -* ]]; then
                # Per-kind flags must match the Go flagset registered in
                # internal/cmd/diff.go (runDiffGeneric / runDiffReplicaSet).
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
            # diff rs revision-diff mode: the positional is the *deployment*
            # name, not the RS name. Everything else uses the typed kind
            # directly — the Go side resolves aliases via the discovery doc.
            local target_kind="${diff_kind}"
            case "${diff_kind}" in
                rs|replicaset) target_kind="deployment" ;;
            esac
            local names
            names="$(kdiag __complete resources "${target_kind}" "${ns}" "${cur}" 2>/dev/null)"
            COMPREPLY=( $(compgen -W "${names}" -- "${cur}") )
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
    esac
}

complete -F _kdiag kdiag
