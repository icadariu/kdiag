package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// runInspectPath implements the generic, kind-agnostic search behind
// `kdiag inspect <kind> [<name>|-l <sel>] --path <needle>`.
//
// It bypasses the kind-specific handlers entirely: resources are fetched via
// the dynamic client as Unstructured, then walked as nested maps/slices so
// the same code path serves Pods, workloads, Nodes, and CRDs.
//
// Output shape: one yq-path per line. When the match sits inside a multi-element
// named array (containers, ports, volumes, …), the line is preceded by a
// `# name=<n>` header for the regroup pass to consume. Identical blocks are
// deduplicated.
//
// Key-match recursion: when a needle matches a key, the walker emits the
// match AND descends into the value, so a common needle like `name` will
// surface every nested occurrence. This is intentional — `--path` is
// grep-like, not "deepest-match-only".
//
// Smart-case matching: an all-lowercase needle is case-insensitive; any
// uppercase character makes the match case-sensitive.
func runInspectPath(env *kube.KubeEnv, kind, name, selector, needle string) {
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
	kindLower := strings.ToLower(resolved.GVK.Kind)
	for i := range items {
		obj := items[i]
		// Two documents are searched per resource: the raw API object (what
		// `kubectl get -o yaml` shows) and kdiag's curated `--yaml` view (which
		// synthesizes fields like `tag` that the raw object lacks). Their paths
		// reference different documents, so each set prints under a header
		// naming the command that produces it.
		rawMatches := walkYMLPath(obj.Object, "", "", needle, smart)
		var kdiagMatches []string
		if view, ok := kdiagViewForObject(env, kind, &obj); ok {
			if m, err := toGenericMap(view); err == nil {
				kdiagMatches = walkYMLPath(m, "", "", needle, smart)
			}
		}
		if len(rawMatches) == 0 && len(kdiagMatches) == 0 {
			continue
		}
		indent := ""
		if header {
			fmt.Printf("%s/%s:\n", resolved.GVK.Kind, obj.GetName())
			indent = "  "
		}
		printPathSection(indent, fmt.Sprintf("# kubectl get %s %s -o yaml", kindLower, obj.GetName()), rawMatches)
		printPathSection(indent, fmt.Sprintf("# kdiag inspect %s %s --yaml", kindLower, obj.GetName()), kdiagMatches)
	}
}

// printPathSection prints one labeled result set: a `#` header naming the source
// document, then the regrouped yq paths (named-array elements nested under their
// `name`). Nothing is printed when matches is empty, so a section header only
// appears when that document actually contained the needle.
func printPathSection(indent, header string, matches []string) {
	if len(matches) == 0 {
		return
	}
	fmt.Printf("%s%s\n", indent, header)
	for _, g := range regroupByName(matches) {
		if g.name == "" {
			for _, p := range g.paths {
				fmt.Printf("%s%s\n", indent, p)
			}
			continue
		}
		fmt.Printf("%s%s:\n", indent, g.name)
		for _, p := range g.paths {
			fmt.Printf("%s  %s\n", indent, p)
		}
	}
}

// kdiagViewForObject returns the curated `--yaml` payload kdiag would emit for
// this resource, so `--path` can search it alongside the raw object. The
// already-fetched Unstructured is converted to its typed form (no extra Get)
// and handed to the same builders the `--yaml` handlers use, guaranteeing the
// searched document matches `--yaml` byte-for-byte. Workloads still list pods
// (a separate resource), exactly as `--yaml` does.
//
// Returns ok=false for kinds without a curated view (CRDs, unknown kinds) or on
// any conversion/fetch error — the caller then falls back to raw-only output.
func kdiagViewForObject(env *kube.KubeEnv, kind string, obj *unstructured.Unstructured) (any, bool) {
	conv := runtime.DefaultUnstructuredConverter
	ctx := context.Background()
	switch kube.CanonicalKind(kind) {
	case "pod":
		var p corev1.Pod
		if err := conv.FromUnstructured(obj.Object, &p); err != nil {
			return nil, false
		}
		return podInfoFrom(p), true
	case "deployment":
		var d appsv1.Deployment
		if err := conv.FromUnstructured(obj.Object, &d); err != nil {
			return nil, false
		}
		return deployWorkloadInfo(env, &d, listDeployPods(env, ctx, &d)), true
	case "daemonset":
		var d appsv1.DaemonSet
		if err := conv.FromUnstructured(obj.Object, &d); err != nil {
			return nil, false
		}
		return workloadInfoForSelector(env, ctx, "DaemonSet", d.Name, daemonsetSummary(&d), d.Spec.Selector), true
	case "statefulset":
		var s appsv1.StatefulSet
		if err := conv.FromUnstructured(obj.Object, &s); err != nil {
			return nil, false
		}
		return workloadInfoForSelector(env, ctx, "StatefulSet", s.Name, statefulsetSummary(&s), s.Spec.Selector), true
	case "replicaset":
		var r appsv1.ReplicaSet
		if err := conv.FromUnstructured(obj.Object, &r); err != nil {
			return nil, false
		}
		return workloadInfoForSelector(env, ctx, "ReplicaSet", r.Name, replicasetSummary(&r), r.Spec.Selector), true
	case "node":
		var n corev1.Node
		if err := conv.FromUnstructured(obj.Object, &n); err != nil {
			return nil, false
		}
		return nodeInfoFrom(n), true
	default:
		return nil, false
	}
}

// toGenericMap renders a kdiag view struct as the same map[string]any the
// walker traverses for raw objects. Going through JSON tags makes the walked
// keys identical to `--yaml` output, which marshals via sigs.k8s.io/yaml
// (also json-tag based).
func toGenericMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// walkYMLPath walks node (a map[string]any or []any from
// unstructured.Object), accumulating one line per matching key or scalar
// value.
//
// path is the yq-compatible path built so far; array elements use `[N]`
// (concrete index) so callers can directly reference the element that matched.
// arrayCtx is the most recent enclosing array element's `name` field —
// when set, the line is preceded by a `# name=<n>` header for the regroup
// pass to consume. Identical emitted lines are deduplicated (siblings under
// an unnamed array that produce the same path collapse to one). Pass "" at
// the top level.
//
// Match semantics (see makeMatcher): a needle without `*` matches the full
// key or scalar value exactly (so `name` does not match `namespace`); use
// `*name*` for substring, `name*` for prefix, etc. Smart-case still applies.
// Reachable via `--path` (see runInspectPath) or `kdiag help yml-path`.
func walkYMLPath(node any, path, arrayCtx, needle string, smart bool) []string {
	match := makeMatcher(needle, smart)
	var out []string
	walkYMLPathInto(node, path, arrayCtx, match, &out)
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

// ymlGroup buckets paths that share an enclosing named array element.
// An empty name means "no name annotation" — those paths print flat,
// before any named blocks.
type ymlGroup struct {
	name  string
	paths []string
}

// regroupByName turns walker output into ordered groups. The walker emits
// either "<path>" (no array ctx) or "# name=<n>\n<path>" (inside named
// array); we split on the embedded newline and bucket by name. The empty
// (ungrouped) bucket is always first when present; named buckets follow
// in first-seen order so output mirrors traversal.
func regroupByName(lines []string) []ymlGroup {
	if len(lines) == 0 {
		return nil
	}
	idx := map[string]int{}
	var groups []ymlGroup
	add := func(name, path string) {
		if i, ok := idx[name]; ok {
			groups[i].paths = append(groups[i].paths, path)
			return
		}
		idx[name] = len(groups)
		groups = append(groups, ymlGroup{name: name, paths: []string{path}})
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "# name=") {
			nl := strings.IndexByte(line, '\n')
			if nl < 0 {
				// Defensive: walker always pairs a header with a path. If
				// this ever produces a headerless line, treat it as
				// ungrouped so we don't drop data.
				add("", line)
				continue
			}
			name := strings.TrimPrefix(line[:nl], "# name=")
			add(name, line[nl+1:])
		} else {
			add("", line)
		}
	}
	return groups
}

func walkYMLPathInto(node any, path, arrayCtx string, match func(string) bool, out *[]string) {
	switch v := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			childPath := path + formatKeyPath(k)
			// Skip server-side-apply ownership bookkeeping — its synthetic
			// `f:`/`k:` keys mirror real field names (image, spec, containers)
			// and would otherwise dominate every --yml-path result.
			if childPath == ".metadata.managedFields" {
				continue
			}
			if match(k) {
				*out = append(*out, emitPath(childPath, arrayCtx))
			}
			walkYMLPathInto(v[k], childPath, arrayCtx, match, out)
		}
	case []any:
		// Name annotation is only useful when there's more than one element
		// to disambiguate — a single-container deployment has nothing to
		// disambiguate, so suppress it.
		multi := len(v) > 1
		for idx, elem := range v {
			childPath := fmt.Sprintf("%s[%d]", path, idx)
			childCtx := arrayCtx
			if multi {
				if m, ok := elem.(map[string]any); ok {
					if n, ok := m["name"].(string); ok && n != "" {
						childCtx = n
					}
				}
			}
			walkYMLPathInto(elem, childPath, childCtx, match, out)
		}
	default:
		if v == nil {
			return
		}
		s := scalarString(v)
		if match(s) {
			*out = append(*out, emitPath(path, arrayCtx))
		}
	}
}

// emitPath renders a single match as a yq-path. Outside an array element
// it returns the path. Inside a named array element it returns a two-line
// block: a leading `# name=<ctx>` header followed by the path line, so the
// container/port/volume name reads naturally above its match. The two lines
// share one slice entry (joined by `\n`) so the existing line-dedup
// naturally dedups whole blocks.
func emitPath(path string, arrayCtx string) string {
	if arrayCtx != "" {
		return fmt.Sprintf("# name=%s\n%s", arrayCtx, path)
	}
	return path
}

// scalarString stringifies a scalar for value-side matching. Booleans and
// numbers stringify to their canonical form so `--path true` and
// `--path 3` work as users expect.
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

// makeMatcher compiles needle into a predicate against a single string.
//
// Rules:
//   - empty needle → never matches.
//   - needle contains `*` → glob: `*` expands to `.*`, everything else
//     is literal, and the whole string must match end-to-end. So `name*`
//     matches "namespace" but not "podname"; `*name*` matches both.
//   - no `*` → exact full-string match (no substring). So `name` matches
//     the key "name" or a value "name", but does NOT match "namespace",
//     "generateName", or "container-1-tiny".
//
// Smart-case still applies on top: an all-lowercase needle matches
// case-insensitively; any uppercase makes the match case-sensitive.
func makeMatcher(needle string, smart bool) func(string) bool {
	if needle == "" {
		return func(string) bool { return false }
	}
	if strings.ContainsRune(needle, '*') {
		var sb strings.Builder
		sb.WriteString(`^`)
		for _, r := range needle {
			if r == '*' {
				sb.WriteString(`.*`)
			} else {
				sb.WriteString(regexp.QuoteMeta(string(r)))
			}
		}
		sb.WriteString(`$`)
		// `(?s)` lets `.*` cross newlines so `*line2*` matches a multi-line
		// scalar that contains "line2" on any line.
		pattern := "(?s)" + sb.String()
		if smart {
			pattern = "(?i)" + pattern
		}
		re := regexp.MustCompile(pattern)
		return func(s string) bool { return re.MatchString(s) }
	}
	if smart {
		ln := strings.ToLower(needle)
		return func(s string) bool { return strings.ToLower(s) == ln }
	}
	return func(s string) bool { return s == needle }
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
