package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// sortRow is the flat shape every kind is reduced to before printing — so the
// formatting code does not need to know about kind-specific types.
type sortRow struct {
	namespace string
	name      string
	created   time.Time
}

// RunSort implements `kdiag sort <kind>`. Lists resources of the given kind
// sorted by creationTimestamp ascending (oldest first, newest last — the same
// orientation as `kubectl logs`, so the most recently created entry is the
// one a human eye naturally lands on when scrolling).
//
// The kind is resolved against the cluster's discovery information, so any
// resource the API server exposes — built-in or CRD — is accepted. Shortnames,
// plurals, singulars, and fully qualified forms (`certificates.cert-manager.io`)
// all work.
func RunSort(args []string) {
	if cli.WantsHelp(args) {
		cli.PrintSortUsage(os.Stdout)
		return
	}

	fs := pflag.NewFlagSet("sort", pflag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (defaults to current context)")
	var allNamespaces bool
	fs.BoolVarP(&allNamespaces, "all-namespaces", "A", false, "list resources across all namespaces (overrides -n)")
	fs.Usage = func() { cli.PrintSortUsage(os.Stderr) }

	// Locate the kind token, skipping flags + their values so `-n foo pod`
	// and `pod -n foo` both work. Mirrors the parser used by inspect.
	kindIdx := sortKindIndex(args)
	if kindIdx < 0 {
		fmt.Fprintln(os.Stderr, "Error: sort requires a kind (e.g. pod, deploy, cm, svc, ingress, or any CRD)")
		fmt.Fprintln(os.Stderr)
		cli.PrintSortUsage(os.Stderr)
		os.Exit(1)
	}
	kindRaw := args[kindIdx]
	rest := append(args[:kindIdx:kindIdx], args[kindIdx+1:]...)
	_ = fs.Parse(rest)

	env, err := kube.NewKubeEnv(k)
	if err != nil {
		cli.Fatal(err)
	}

	resolved, err := kube.ResolveResource(env.Mapper, kindRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: unknown sort kind %q: %v\n\n", kindRaw, err)
		cli.PrintSortUsage(os.Stderr)
		os.Exit(1)
	}

	ctx := context.Background()

	// Cluster-scoped kinds ignore namespace flags entirely; -A on a
	// namespaced kind widens to all namespaces.
	listNs := env.Namespace
	switch {
	case !resolved.Namespaced:
		listNs = ""
	case allNamespaces:
		listNs = ""
	}

	rows, err := collectSortRows(ctx, env, resolved, listNs)
	if err != nil {
		cli.Fatal(err)
	}

	// Ascending: oldest first, newest last (like `kubectl logs`).
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].created.Before(rows[j].created)
	})

	scope := "Namespace: " + env.Namespace
	switch {
	case !resolved.Namespaced:
		scope = "Scope: cluster"
	case allNamespaces:
		scope = "Namespace: <all>"
	}
	fmt.Printf("%s\nKind: %s\n\n", scope, displayKind(resolved.GVK))

	if len(rows) == 0 {
		fmt.Println("No resources found.")
		return
	}

	tw := cli.NewTabWriter(os.Stdout)
	showNamespaceCol := allNamespaces && resolved.Namespaced
	if showNamespaceCol {
		fmt.Fprintln(tw, "AGE\tCREATED\tNAMESPACE\tNAME")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				cli.FormatAge(r.created), r.created.UTC().Format(time.RFC3339), r.namespace, r.name)
		}
	} else {
		fmt.Fprintln(tw, "AGE\tCREATED\tNAME")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\n",
				cli.FormatAge(r.created), r.created.UTC().Format(time.RFC3339), r.name)
		}
	}
	_ = tw.Flush()
}

// collectSortRows lists resources of the resolved GVR via the dynamic client
// and projects them onto sortRow. ns="" lists across all namespaces (or is
// the only valid value for cluster-scoped kinds).
func collectSortRows(ctx context.Context, env *kube.KubeEnv, r *kube.ResolvedResource, ns string) ([]sortRow, error) {
	ri := env.Dynamic.Resource(r.GVR)
	opts := kube.ListOptions("")
	if r.Namespaced {
		ul, err := ri.Namespace(ns).List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", r.GVR.Resource, err)
		}
		return rowsFromUnstructured(ul.Items), nil
	}
	ul, err := ri.List(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", r.GVR.Resource, err)
	}
	return rowsFromUnstructured(ul.Items), nil
}

// rowsFromUnstructured projects unstructured.Unstructured items onto sortRow
// using the standard ObjectMeta accessors that every Kubernetes object
// satisfies — no kind-specific knowledge required.
func rowsFromUnstructured(items []unstructured.Unstructured) []sortRow {
	rows := make([]sortRow, len(items))
	for i := range items {
		rows[i] = sortRow{
			namespace: items[i].GetNamespace(),
			name:      items[i].GetName(),
			created:   items[i].GetCreationTimestamp().Time,
		}
	}
	return rows
}

// displayKind renders the singular Kind (lowercased) for built-in core/v1
// kinds — preserving the kubectl-style banner users expect ("pod",
// "deployment", "node"). Group-qualified kinds (anything outside the empty
// group) include the group as a suffix to disambiguate ("deployment.apps",
// "widget.demo.example.com").
func displayKind(gvk schema.GroupVersionKind) string {
	kind := strings.ToLower(gvk.Kind)
	if gvk.Group == "" {
		return kind
	}
	return kind + "." + gvk.Group
}

// sortKindIndex returns the index of the first non-flag token in args, skipping
// flags and their values. Mirrors inspect.kindIndex but covers the flags this
// command actually accepts.
func sortKindIndex(args []string) int {
	valueFlags := map[string]bool{
		"--namespace": true, "-n": true,
	}
	for i := 0; i < len(args); i++ {
		if valueFlags[args[i]] {
			i++
			continue
		}
		if len(args[i]) > 0 && args[i][0] == '-' {
			continue
		}
		return i
	}
	return -1
}

