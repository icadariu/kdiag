package cmd

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// runInspectFindPath implements the generic, kind-agnostic search behind
// `kdiag inspect <kind> [<name>|-l <sel>] --find-path <needle>`.
//
// It bypasses the kind-specific handlers entirely: resources are fetched via
// the dynamic client as Unstructured, then walked as nested maps/slices so
// the same code path serves Pods, workloads, Nodes, and CRDs.
//
// Output shape: `<yq-path>: <value>` per line for plain matches. When the
// match sits inside a multi-element named array (containers, ports,
// volumes, …), the line is preceded by a `# name=<n>` header so siblings
// stay distinguishable. Multi-line string values are Go-quoted (`%q`) so
// every emitted match stays on one line — readers can re-run `yq <path>`
// to see the raw value. Identical blocks are deduplicated.
//
// Key-match recursion: when a needle matches a key, the walker emits the
// match AND descends into the value, so a common needle like `name` will
// surface every nested occurrence. This is intentional — `--find-path`
// is grep-like, not "deepest-match-only".
//
// Smart-case matching: an all-lowercase needle is case-insensitive; any
// uppercase character makes the match case-sensitive.
func runInspectFindPath(env *kube.KubeEnv, kind, name, selector, needle string) {
	resolved, err := kube.ResolveResource(env.Mapper, kind)
	if err != nil {
		cli.Fatal(fmt.Errorf("resolve %s: %w", kind, err))
	}
	ctx := context.Background()
	ri := env.Dynamic.Resource(resolved.GVR)

	var items []unstructured.Unstructured
	if name != "" {
		var obj *unstructured.Unstructured
		if resolved.Namespaced {
			obj, err = ri.Namespace(env.Namespace).Get(ctx, name, kube.GetOptions())
		} else {
			obj, err = ri.Get(ctx, name, kube.GetOptions())
		}
		if err != nil {
			cli.Fatal(fmt.Errorf("get %s/%s: %w", kind, name, err))
		}
		items = []unstructured.Unstructured{*obj}
	} else {
		var list *unstructured.UnstructuredList
		if resolved.Namespaced {
			list, err = ri.Namespace(env.Namespace).List(ctx, kube.ListOptions(selector))
		} else {
			list, err = ri.List(ctx, kube.ListOptions(selector))
		}
		if err != nil {
			cli.Fatal(fmt.Errorf("list %s: %w", kind, err))
		}
		items = list.Items
	}

	smart := isAllLower(needle)
	// Selector mode always uses per-resource headers so output stays
	// unambiguous even when only one resource matches. Name-lookup mode
	// targets exactly one resource and prints matches flat.
	header := name == ""
	for i := range items {
		obj := items[i]
		matches := walkFindPath(obj.Object, "", "", needle, smart)
		if len(matches) == 0 {
			continue
		}
		if header {
			fmt.Printf("%s/%s:\n", resolved.GVK.Kind, obj.GetName())
			for _, m := range matches {
				for _, line := range strings.Split(m, "\n") {
					fmt.Printf("  %s\n", line)
				}
			}
		} else {
			for _, m := range matches {
				fmt.Println(m)
			}
		}
	}
}

// walkFindPath walks node (a map[string]any or []any from
// unstructured.Object), accumulating one line per matching key or scalar
// value.
//
// path is the yq-compatible path built so far; array elements use `[]`
// rather than `[N]` so the emitted path is directly yq-pipeable (iterate).
// arrayCtx is the most recent enclosing array element's `name` field —
// when set, the value moves into a trailing `# name=<n>: <value>` comment
// so callers can still tell which container/port/volume produced the line.
// Identical emitted lines are deduplicated (siblings under an unnamed
// array that produce the same path+value collapse to one). Pass "" at the
// top level.
func walkFindPath(node any, path, arrayCtx, needle string, smart bool) []string {
	var out []string
	walkFindPathInto(node, path, arrayCtx, needle, smart, &out)
	return dedupStable(out)
}

// dedupStable returns in with consecutive-or-distant duplicates removed,
// preserving first-occurrence order.
func dedupStable(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func walkFindPathInto(node any, path, arrayCtx, needle string, smart bool, out *[]string) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			childPath := path + formatKeyPath(k)
			if substrMatch(k, needle, smart) {
				*out = append(*out, emitLine(childPath, v[k], arrayCtx))
			}
			walkFindPathInto(v[k], childPath, arrayCtx, needle, smart, out)
		}
	case []any:
		childPath := path + "[]"
		// Name annotation is only useful when there's more than one
		// element to disambiguate — a single-container deployment has
		// nothing to disambiguate, so suppress it.
		multi := len(v) > 1
		for _, elem := range v {
			childCtx := arrayCtx
			if multi {
				if m, ok := elem.(map[string]any); ok {
					if n, ok := m["name"].(string); ok && n != "" {
						childCtx = n
					}
				}
			}
			walkFindPathInto(elem, childPath, childCtx, needle, smart, out)
		}
	default:
		if v == nil {
			return
		}
		s := scalarString(v)
		if substrMatch(s, needle, smart) {
			*out = append(*out, emitLine(path, v, arrayCtx))
		}
	}
}

// emitLine renders a single match. Outside an array element it returns
// one line: `<path>: <value>`. Inside a named array element it returns a
// two-line block: a leading `# name=<ctx>` header followed by the
// `<path>: <value>` line, so the container/port/volume name reads
// naturally above its match. The two lines share one slice entry (joined
// by `\n`) so the existing line-dedup naturally dedups whole blocks.
func emitLine(path string, v any, arrayCtx string) string {
	if arrayCtx != "" {
		return fmt.Sprintf("# name=%s\n%s: %s", arrayCtx, path, formatYQValue(v))
	}
	return fmt.Sprintf("%s: %s", path, formatYQValue(v))
}

// formatYQValue renders v for the right-hand side of a path-value line.
// Single-line scalars print verbatim. Strings that contain newlines are
// Go-quoted (`%q`) so a multi-line ConfigMap value stays on one line and
// can't bleed into the next emitted match. Maps and slices collapse to
// `<object>` / `<array>` so the line stays compact — the user can re-run
// `yq <path>` to expand.
func formatYQValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		if strings.ContainsAny(x, "\n\r") {
			return fmt.Sprintf("%q", x)
		}
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case map[string]any:
		return "<object>"
	case []any:
		return "<array>"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// scalarString stringifies a scalar for value-side matching. Booleans and
// numbers stringify to their canonical form so `--find-path true` and
// `--find-path 3` work as users expect.
func scalarString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// substrMatch is the smart-case substring matcher. When smart is true (needle
// is all-lowercase) both sides are lowercased; otherwise it's a literal
// substring check.
func substrMatch(haystack, needle string, smart bool) bool {
	if needle == "" {
		return false
	}
	if smart {
		return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
	}
	return strings.Contains(haystack, needle)
}

func isAllLower(s string) bool {
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			return false
		}
	}
	return true
}

var yqIdentRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// formatKeyPath renders a map key as a yq path component. Identifier-safe
// keys use the dot form (`.foo`); anything else (kebab-case, dotted,
// containing slashes) uses the bracket-quoted form (`["foo.bar/baz"]`) so
// the resulting path is copy-pasteable into yq.
func formatKeyPath(k string) string {
	if yqIdentRE.MatchString(k) {
		return "." + k
	}
	return fmt.Sprintf("[%q]", k)
}
