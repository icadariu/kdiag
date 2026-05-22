package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

const revisionAnnotation = "deployment.kubernetes.io/revision"

// RunDiff dispatches `kdiag diff <kind> ...`.
//
// Two shapes coexist:
//   - Revision diff (replicaset only): `diff rs <deploy>`, `diff rs -l <sel>`,
//     `diff rs <deploy> <rev-from> <rev-to>` — compare pod template specs
//     between two revisions of a deployment.
//   - Generic two-name diff: `diff <any-kind> <name1> <name2>` — compare two
//     resources of the same kind by name. Works for any kind the API server
//     exposes, built-in or CRD.
//
// The user-facing `--full` flag turns off every kdiag-side massaging step
// (per-kind noise stripping, .spec.template subtree extraction) and emits whatever
// the API server returned, marshaled to YAML byte-for-byte.
func RunDiff(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Error: diff requires a kind")
		fmt.Fprintln(os.Stderr)
		cli.PrintDiffUsage(os.Stderr)
		os.Exit(1)
	}
	if cli.WantsHelp(args) {
		cli.PrintDiffUsage(os.Stdout)
		return
	}

	kindIdx := kindIndex(args)
	if kindIdx < 0 {
		fmt.Fprintln(os.Stderr, "Error: diff requires a kind")
		fmt.Fprintln(os.Stderr)
		cli.PrintDiffUsage(os.Stderr)
		os.Exit(1)
	}
	kind := args[kindIdx]
	handlerArgs := append(args[:kindIdx:kindIdx], args[kindIdx+1:]...)

	// Replicaset has the additional revision-diff shape. Two explicit names
	// go to the generic path; anything else (1 name, -l selector, 3 names)
	// is revision diff.
	if kube.CanonicalKind(kind) == "replicaset" && isRSRevisionShape(handlerArgs) {
		runDiffReplicaSet(handlerArgs)
		return
	}

	runDiffGeneric(kind, handlerArgs)
}

// isRSRevisionShape returns true unless handlerArgs after flag parsing
// resolves to "exactly two positional names and no -l selector" — the one
// shape that should route to the generic two-name diff.
func isRSRevisionShape(args []string) bool {
	fs := pflag.NewFlagSet("rs-shape-peek", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var ns, selector string
	var full bool
	fs.StringVarP(&ns, "namespace", "n", "", "")
	fs.StringVarP(&selector, "label", "l", "", "")
	fs.BoolVar(&full, "full", false, "")
	if err := fs.Parse(args); err != nil {
		// Parse errors will surface in the real handler — bias toward the
		// revision path so the user sees the rs-specific error message.
		return true
	}
	return !(selector == "" && fs.NArg() == 2)
}

// runDiffGeneric is the any-kind two-name diff path. It resolves the
// user-typed kind via the cluster's discovery doc, fetches each resource
// through the dynamic client (which preserves apiVersion/kind/managedFields
// from the wire), and runs `diff -u --color=always` over the YAML.
func runDiffGeneric(kindRaw string, args []string) {
	fs := pflag.NewFlagSet("diff", pflag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (defaults to current context)")
	var full bool
	fs.BoolVar(&full, "full", false, "show the raw API server response (no per-kind noise stripping)")
	fs.Usage = func() { cli.PrintDiffKindUsage(os.Stderr, kindRaw) }

	if cli.WantsHelp(args) {
		cli.PrintDiffKindUsage(os.Stdout, kindRaw)
		return
	}
	_ = fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintf(os.Stderr, "Error: diff %s requires exactly two resource names\n", kindRaw)
		fmt.Fprintf(os.Stderr, "Run 'kdiag diff %s --help' for usage.\n", kindRaw)
		os.Exit(1)
	}

	env, err := kube.NewKubeEnv(k)
	if err != nil {
		cli.Fatal(err)
	}

	resolved, err := kube.ResolveResource(env.Mapper, kindRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: unknown diff kind %q: %v\n", kindRaw, err)
		os.Exit(1)
	}

	ctx := context.Background()
	obj1, err := fetchGeneric(ctx, env, resolved, rest[0])
	if err != nil {
		cli.Fatal(fmt.Errorf("get %s %q: %w", resolved.GVR.Resource, rest[0], err))
	}
	obj2, err := fetchGeneric(ctx, env, resolved, rest[1])
	if err != nil {
		cli.Fatal(fmt.Errorf("get %s %q: %w", resolved.GVR.Resource, rest[1], err))
	}

	if !full {
		stripKindNoise(obj1, resolved.GVK)
		stripKindNoise(obj2, resolved.GVK)
	}

	y1, err := yaml.Marshal(obj1.Object)
	if err != nil {
		cli.Fatal(fmt.Errorf("marshal %s: %w", resolved.GVR.Resource, err))
	}
	y2, err := yaml.Marshal(obj2.Object)
	if err != nil {
		cli.Fatal(fmt.Errorf("marshal %s: %w", resolved.GVR.Resource, err))
	}

	label := displayKind(resolved.GVK)
	printScopeBanner(env.Namespace, resolved.Namespaced)
	fmt.Printf("Diff: %s/%s vs %s/%s\n\n", label, rest[0], label, rest[1])

	runUnifiedDiff(y1, y2,
		fmt.Sprintf("%s/%s", label, rest[0]),
		fmt.Sprintf("%s/%s", label, rest[1]),
	)
}

func fetchGeneric(ctx context.Context, env *kube.KubeEnv, r *kube.ResolvedResource, name string) (*unstructured.Unstructured, error) {
	ri := env.Dynamic.Resource(r.GVR)
	if r.Namespaced {
		return ri.Namespace(env.Namespace).Get(ctx, name, kube.GetOptions())
	}
	return ri.Get(ctx, name, kube.GetOptions())
}

// stripNoise removes fields that always differ between two distinct
// resources but say nothing about what's actually different — etcd
// bookkeeping (resourceVersion, uid, generation, creationTimestamp), SSA
// metadata (managedFields), the duplicated last-applied JSON blob, and
// per-container runtime IDs. Keeps everything that helps an investigator
// (labels, annotations, owner refs, spec, all status timestamps, IPs,
// restart counts, conditions, replica counts, selectors).
//
// `--full` is the escape hatch for users who want the raw API server
// response without any of this filtering.
func stripNoise(o *unstructured.Unstructured) {
	for _, f := range []string{
		"managedFields",
		"resourceVersion",
		"uid",
		"generation",
		"creationTimestamp",
		// selfLink is deprecated but still echoed by some apiservers.
		"selfLink",
	} {
		unstructured.RemoveNestedField(o.Object, "metadata", f)
	}

	// The "last-applied-configuration" annotation is a single huge JSON line
	// duplicating the spec — it's always different between two distinct
	// resources and never adds information that isn't already elsewhere.
	if annos, found, _ := unstructured.NestedStringMap(o.Object, "metadata", "annotations"); found {
		delete(annos, "kubectl.kubernetes.io/last-applied-configuration")
		if len(annos) == 0 {
			unstructured.RemoveNestedField(o.Object, "metadata", "annotations")
		} else {
			_ = unstructured.SetNestedStringMap(o.Object, annos, "metadata", "annotations")
		}
	}

	// status.observedGeneration tracks internal reconciliation progress.
	unstructured.RemoveNestedField(o.Object, "status", "observedGeneration")

	// Container runtime IDs (`containerID`, `lastState.terminated.containerID`)
	// are random per-pod identifiers that always differ — never diagnostic.
	for _, key := range []string{"containerStatuses", "initContainerStatuses", "ephemeralContainerStatuses"} {
		stripContainerRuntimeIDs(o.Object, "status", key)
	}
}

func stripContainerRuntimeIDs(obj map[string]any, path ...string) {
	arr, found, _ := unstructured.NestedSlice(obj, path...)
	if !found {
		return
	}
	for i := range arr {
		item, ok := arr[i].(map[string]any)
		if !ok {
			continue
		}
		delete(item, "containerID")
		if lastState, ok := item["lastState"].(map[string]any); ok {
			if term, ok := lastState["terminated"].(map[string]any); ok {
				delete(term, "containerID")
			}
		}
		arr[i] = item
	}
	_ = unstructured.SetNestedSlice(obj, arr, path...)
}

// stripKindNoise applies per-kind cleanup on top of the generic stripNoise baseline.
// It removes fields that say nothing about what an investigator cares about for that kind.
func stripKindNoise(o *unstructured.Unstructured, gvk schema.GroupVersionKind) {
	// Always strip the generic baseline first.
	stripNoise(o)

	switch gvk.Kind {
	case "Pod":
		stripPodNoise(o)
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		stripWorkloadNoise(o)
	case "Service":
		stripServiceNoise(o)
	case "ConfigMap":
		// No per-kind stripping needed beyond baseline for ConfigMap.
	case "Secret":
		// No per-kind stripping needed beyond baseline for Secret.
	case "Node":
		stripNodeNoise(o)
	case "Ingress":
		stripIngressNoise(o)
	case "PersistentVolumeClaim":
		stripPVCNoise(o)
	case "PersistentVolume":
		stripPVNoise(o)
	default:
		// For unknown kinds (CRDs, etc.), the baseline stripNoise is sufficient.
	}
}

func stripPodNoise(o *unstructured.Unstructured) {
	// Strip entire status.
	unstructured.RemoveNestedField(o.Object, "status")

	// Strip spec.nodeName (pods land on different nodes).
	unstructured.RemoveNestedField(o.Object, "spec", "nodeName")

	// Strip pod-template-hash and other controller labels.
	stripLabels(o, []string{
		"pod-template-hash",
		"controller-revision-hash",
		"statefulset.kubernetes.io/pod-name",
		"apps.kubernetes.io/pod-index",
	}, "metadata", "labels")

	// Strip ownerReferences (different RS hash per deploy revision).
	unstructured.RemoveNestedField(o.Object, "metadata", "ownerReferences")

	// Strip generateName (per-pod suffix differs).
	unstructured.RemoveNestedField(o.Object, "metadata", "generateName")

	// Strip name (always differs).
	unstructured.RemoveNestedField(o.Object, "metadata", "name")

	// Strip Kubernetes-auto-injected tolerations.
	stripAutoInjectedTolerations(o)

	// Strip the auto-injected projected service-account token volume
	// (named kube-api-access-<random>) and the matching volumeMount on
	// every container — Kubernetes generates a unique suffix per pod,
	// so this always differs between two pods of the same workload.
	stripSATokenVolume(o)

	// Strip all metadata annotations (too noisy).
	unstructured.RemoveNestedField(o.Object, "metadata", "annotations")
}

func stripWorkloadNoise(o *unstructured.Unstructured) {
	// Strip entire status.
	unstructured.RemoveNestedField(o.Object, "status")

	// Strip deployment.kubernetes.io/revision annotation.
	stripAnnotations(o, []string{
		"deployment.kubernetes.io/revision",
	}, "metadata", "annotations")

	// Strip spec.template.metadata.creationTimestamp (always null but renders).
	unstructured.RemoveNestedField(o.Object, "spec", "template", "metadata", "creationTimestamp")

	// Strip pod-template-hash from selectors and labels.
	stripLabels(o, []string{"pod-template-hash"}, "spec", "selector", "matchLabels")
	stripLabels(o, []string{"pod-template-hash"}, "spec", "template", "metadata", "labels")
}

func stripServiceNoise(o *unstructured.Unstructured) {
	// Strip entire status.
	unstructured.RemoveNestedField(o.Object, "status")

	// Strip cluster-assigned fields.
	unstructured.RemoveNestedField(o.Object, "spec", "clusterIP")
	unstructured.RemoveNestedField(o.Object, "spec", "clusterIPs")
	unstructured.RemoveNestedField(o.Object, "spec", "ipFamilies")
	unstructured.RemoveNestedField(o.Object, "spec", "ipFamilyPolicy")
	unstructured.RemoveNestedField(o.Object, "spec", "internalTrafficPolicy")

	// Strip auto-assigned nodePort from each port entry.
	// Access ports directly from the object map to avoid deep copy issues.
	if spec, ok := o.Object["spec"].(map[string]any); ok {
		if ports, ok := spec["ports"].([]any); ok {
			for i := range ports {
				if port, ok := ports[i].(map[string]any); ok {
					delete(port, "nodePort")
				}
			}
		}
	}
}

func stripNodeNoise(o *unstructured.Unstructured) {
	// Strip entire status (capacity, conditions, addresses, nodeInfo, etc.).
	unstructured.RemoveNestedField(o.Object, "status")
}

func stripIngressNoise(o *unstructured.Unstructured) {
	// Strip entire status (loadBalancer.ingress assigned by controller).
	unstructured.RemoveNestedField(o.Object, "status")
}

func stripPVCNoise(o *unstructured.Unstructured) {
	// Strip entire status.
	unstructured.RemoveNestedField(o.Object, "status")

	// Strip provisioner and controller annotations.
	stripAnnotations(o, []string{
		"volume.kubernetes.io/storage-provisioner",
		"volume.beta.kubernetes.io/storage-provisioner",
		"pv.kubernetes.io/bind-completed",
		"pv.kubernetes.io/bound-by-controller",
	}, "metadata", "annotations")

	// Strip volumeName (matches the PV the provisioner bound).
	unstructured.RemoveNestedField(o.Object, "spec", "volumeName")
}

func stripPVNoise(o *unstructured.Unstructured) {
	// Strip entire status.
	unstructured.RemoveNestedField(o.Object, "status")

	// Strip claimRef resourceVersion and uid.
	if claimRef, found, _ := unstructured.NestedMap(o.Object, "spec", "claimRef"); found {
		delete(claimRef, "resourceVersion")
		delete(claimRef, "uid")
		if len(claimRef) == 0 {
			unstructured.RemoveNestedField(o.Object, "spec", "claimRef")
		} else {
			_ = unstructured.SetNestedMap(o.Object, claimRef, "spec", "claimRef")
		}
	}
}

// stripLabels removes specified keys from the label map at the given path.
// path should be like "metadata", "labels" and keysToDelete are the label keys to remove.
func stripLabels(o *unstructured.Unstructured, keysToDelete []string, path ...string) {
	labels, found, _ := unstructured.NestedStringMap(o.Object, path...)
	if !found {
		return
	}
	for _, key := range keysToDelete {
		delete(labels, key)
	}
	if len(labels) == 0 {
		unstructured.RemoveNestedField(o.Object, path...)
	} else {
		_ = unstructured.SetNestedStringMap(o.Object, labels, path...)
	}
}

// stripAnnotations removes specified annotation keys.
// path should be like "metadata", "annotations" and keysToDelete are the annotation keys to remove.
func stripAnnotations(o *unstructured.Unstructured, keysToDelete []string, path ...string) {
	annos, found, _ := unstructured.NestedStringMap(o.Object, path...)
	if !found {
		return
	}
	for _, key := range keysToDelete {
		delete(annos, key)
	}
	if len(annos) == 0 {
		unstructured.RemoveNestedField(o.Object, path...)
	} else {
		_ = unstructured.SetNestedStringMap(o.Object, annos, path...)
	}
}

// stripSATokenVolume removes the auto-injected projected service-account
// token volume from spec.volumes[] and the matching volumeMount entry from
// every container's spec.containers[].volumeMounts[] and
// spec.initContainers[].volumeMounts[]. Kubernetes names this volume
// `kube-api-access-<random>` and injects it on every pod that has a service
// account (i.e. nearly every pod) — the random suffix differs per pod so
// it always shows up in a two-pod diff.
func stripSATokenVolume(o *unstructured.Unstructured) {
	const prefix = "kube-api-access-"

	filterByName := func(items []any) []any {
		var out []any
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				out = append(out, item)
				continue
			}
			if name, _ := m["name"].(string); strings.HasPrefix(name, prefix) {
				continue
			}
			out = append(out, item)
		}
		return out
	}

	if volumes, found, _ := unstructured.NestedSlice(o.Object, "spec", "volumes"); found {
		filtered := filterByName(volumes)
		if len(filtered) == 0 {
			unstructured.RemoveNestedField(o.Object, "spec", "volumes")
		} else {
			_ = unstructured.SetNestedSlice(o.Object, filtered, "spec", "volumes")
		}
	}

	for _, containerKey := range []string{"containers", "initContainers"} {
		containers, found, _ := unstructured.NestedSlice(o.Object, "spec", containerKey)
		if !found {
			continue
		}
		for i, c := range containers {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			mounts, ok := cm["volumeMounts"].([]any)
			if !ok {
				continue
			}
			filtered := filterByName(mounts)
			if len(filtered) == 0 {
				delete(cm, "volumeMounts")
			} else {
				cm["volumeMounts"] = filtered
			}
			containers[i] = cm
		}
		_ = unstructured.SetNestedSlice(o.Object, containers, "spec", containerKey)
	}
}

// stripAutoInjectedTolerations removes the two Kubernetes-auto-injected tolerations.
func stripAutoInjectedTolerations(o *unstructured.Unstructured) {
	tolerations, found, _ := unstructured.NestedSlice(o.Object, "spec", "tolerations")
	if !found {
		return
	}

	var filtered []any
	for _, item := range tolerations {
		tol, ok := item.(map[string]any)
		if !ok {
			filtered = append(filtered, item)
			continue
		}

		// Keep tolerations that are NOT the auto-injected ones.
		key, _ := tol["key"].(string)
		if key == "node.kubernetes.io/not-ready" || key == "node.kubernetes.io/unreachable" {
			// Skip this one; it's auto-injected.
			continue
		}

		filtered = append(filtered, item)
	}

	if len(filtered) == 0 {
		unstructured.RemoveNestedField(o.Object, "spec", "tolerations")
	} else {
		_ = unstructured.SetNestedSlice(o.Object, filtered, "spec", "tolerations")
	}
}

func printScopeBanner(ns string, namespaced bool) {
	if namespaced {
		fmt.Printf("Namespace: %s\n", ns)
		return
	}
	fmt.Println("Scope: cluster")
}

// runUnifiedDiff writes the two YAML blobs to temp files and execs
// `diff -u --color=always` with the labels passed through verbatim. Exit
// code 1 from `diff` means "files differ" — propagate exit 0 in that case
// because the kdiag caller wants to see the diff, not an error.
func runUnifiedDiff(y1, y2 []byte, label1, label2 string) {
	f1, err := os.CreateTemp("", "kdiag-a-*.yaml")
	if err != nil {
		cli.Fatal(err)
	}
	defer os.Remove(f1.Name())
	if _, err := f1.Write(y1); err != nil {
		cli.Fatal(err)
	}
	f1.Close()

	f2, err := os.CreateTemp("", "kdiag-b-*.yaml")
	if err != nil {
		cli.Fatal(err)
	}
	defer os.Remove(f2.Name())
	if _, err := f2.Write(y2); err != nil {
		cli.Fatal(err)
	}
	f2.Close()

	diffCmd := exec.Command("diff", "--color=always", "-u",
		"--label", label1,
		"--label", label2,
		f1.Name(), f2.Name())
	diffCmd.Stdout = os.Stdout
	diffCmd.Stderr = os.Stderr
	if err := diffCmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return
		}
		cli.Fatal(fmt.Errorf("diff: %w", err))
	}
}

// runDiffReplicaSet is the revision-diff path. With --full it bypasses
// the .spec.template extraction and dumps the full RS objects (fetched via
// the dynamic client so apiVersion/kind/managedFields appear) — same shape
// as the generic path but anchored to the deployment's revisions.
func runDiffReplicaSet(args []string) {
	fs := pflag.NewFlagSet("diff rs", pflag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (defaults to current context)")
	var selector string
	fs.StringVarP(&selector, "label", "l", "", "label selector to identify the deployment")
	var full bool
	fs.BoolVar(&full, "full", false, "dump the full RS objects instead of just .spec.template")
	fs.Usage = func() { printDiffRSHelp(os.Stderr, fs) }

	if cli.WantsHelp(args) {
		printDiffRSHelp(os.Stdout, fs)
		return
	}
	_ = fs.Parse(args)

	rest := fs.Args()
	depName, revFrom, revTo, hasRevs, err := parseDiffRSArgs(rest, selector)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fs.Usage()
		os.Exit(1)
	}

	env, kerr := kube.NewKubeEnv(k)
	if kerr != nil {
		cli.Fatal(kerr)
	}
	ctx := context.Background()

	var deploy appsv1.Deployment
	if depName != "" {
		d, err := env.Clientset.AppsV1().Deployments(env.Namespace).Get(ctx, depName, kube.GetOptions())
		if err != nil {
			cli.Fatal(fmt.Errorf("get deployment: %w", err))
		}
		deploy = *d
	} else {
		list, err := env.Clientset.AppsV1().Deployments(env.Namespace).List(ctx, kube.ListOptions(selector))
		if err != nil {
			cli.Fatal(fmt.Errorf("list deployments: %w", err))
		}
		if len(list.Items) == 0 {
			fmt.Fprintln(os.Stderr, "Error: no deployments found matching selector")
			os.Exit(1)
		}
		if len(list.Items) > 1 {
			fmt.Fprintf(os.Stderr, "Error: selector matched %d deployments — be more specific\n", len(list.Items))
			for _, d := range list.Items {
				fmt.Fprintf(os.Stderr, "  %s\n", d.Name)
			}
			os.Exit(1)
		}
		deploy = list.Items[0]
	}

	rsSel := metav1.FormatLabelSelector(deploy.Spec.Selector)
	rsList, err := env.Clientset.AppsV1().ReplicaSets(env.Namespace).List(ctx, kube.ListOptions(rsSel))
	if err != nil {
		cli.Fatal(fmt.Errorf("list replicasets: %w", err))
	}

	items := rsList.Items
	sort.Slice(items, func(i, j int) bool {
		ri, _ := strconv.Atoi(items[i].Annotations[revisionAnnotation])
		rj, _ := strconv.Atoi(items[j].Annotations[revisionAnnotation])
		return ri > rj
	})

	var prev, curr appsv1.ReplicaSet
	var prevRev, currRev string
	if hasRevs {
		from := findByRevision(items, revFrom)
		to := findByRevision(items, revTo)
		if from == nil || to == nil {
			fmt.Fprintf(os.Stderr, "Error: revision not found:")
			if from == nil {
				fmt.Fprintf(os.Stderr, " %d", revFrom)
			}
			if to == nil {
				fmt.Fprintf(os.Stderr, " %d", revTo)
			}
			fmt.Fprintln(os.Stderr)
			printAvailableRevisions(os.Stderr, items)
			os.Exit(1)
		}
		prev, curr = *from, *to
		prevRev = strconv.Itoa(revFrom)
		currRev = strconv.Itoa(revTo)
	} else {
		if len(items) < 2 {
			fmt.Fprintln(os.Stderr, "Error: need at least 2 replicasets to diff (only one revision found)")
			os.Exit(1)
		}
		curr = items[0]
		prev = items[1]
		currRev = curr.Annotations[revisionAnnotation]
		prevRev = prev.Annotations[revisionAnnotation]
	}

	var prevYAML, currYAML []byte
	if full {
		// Full RS objects, fetched via the dynamic client so apiVersion/kind
		// and managedFields appear verbatim.
		rsGVR := appsv1.SchemeGroupVersion.WithResource("replicasets")
		prevU, err := env.Dynamic.Resource(rsGVR).Namespace(env.Namespace).Get(ctx, prev.Name, kube.GetOptions())
		if err != nil {
			cli.Fatal(fmt.Errorf("get rs %q: %w", prev.Name, err))
		}
		currU, err := env.Dynamic.Resource(rsGVR).Namespace(env.Namespace).Get(ctx, curr.Name, kube.GetOptions())
		if err != nil {
			cli.Fatal(fmt.Errorf("get rs %q: %w", curr.Name, err))
		}
		prevYAML, err = yaml.Marshal(prevU.Object)
		if err != nil {
			cli.Fatal(fmt.Errorf("marshal previous: %w", err))
		}
		currYAML, err = yaml.Marshal(currU.Object)
		if err != nil {
			cli.Fatal(fmt.Errorf("marshal current: %w", err))
		}
	} else {
		// Pod template only — focused on what changed in the rollout.
		prevYAML, err = yaml.Marshal(prev.Spec.Template)
		if err != nil {
			cli.Fatal(fmt.Errorf("marshal previous: %w", err))
		}
		currYAML, err = yaml.Marshal(curr.Spec.Template)
		if err != nil {
			cli.Fatal(fmt.Errorf("marshal current: %w", err))
		}
	}

	fmt.Printf("Deployment: %s/%s\n", env.Namespace, deploy.Name)
	fmt.Printf("Diff: revision %s → %s\n\n", prevRev, currRev)

	runUnifiedDiff(prevYAML, currYAML,
		fmt.Sprintf("revision/%s (%s)", prevRev, prev.Name),
		fmt.Sprintf("revision/%s (%s)", currRev, curr.Name),
	)
}

func printDiffRSHelp(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  kdiag diff rs [flags] <deployment-name> [<rev-from> <rev-to>]")
	fmt.Fprintln(w, "  kdiag diff rs [flags] -l <label> [<rev-from> <rev-to>]")
	fmt.Fprintln(w, "  kdiag diff rs [flags] <rs-name-a> <rs-name-b>     (generic two-name form)")
	fmt.Fprintln(w, "\nDiff pod template spec between two ReplicaSet revisions of a deployment.")
	fmt.Fprintln(w, "Without revisions, diffs the previous and current (last two).")
	fmt.Fprintln(w, "With --full, dumps the full RS objects (managedFields preserved).")
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, fs.FlagUsages())
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  kdiag diff rs -n my-ns my-deployment              # last two revisions")
	fmt.Fprintln(w, "  kdiag diff rs -n my-ns my-deployment 2 5          # specific revisions")
	fmt.Fprintln(w, "  kdiag diff rs -n my-ns -l app=my-app 1 3          # via selector")
	fmt.Fprintln(w, "  kdiag diff rs -n my-ns my-rs-abc my-rs-def        # two RS by name")
}

// parseDiffRSArgs maps the trailing positionals after `kdiag diff rs` onto
// (deployment-name, rev-from, rev-to). With -l/--label, the deployment
// is resolved separately so positionals are 0 or 2 (the rev pair). Without
// -l, positional 1 is the deployment name and an optional pair follows.
// The 2-args-no-selector shape is excluded from this path (RunDiff routes
// it to the generic handler).
func parseDiffRSArgs(rest []string, selector string) (depName string, revFrom, revTo int, hasRevs bool, err error) {
	switch {
	case selector == "" && len(rest) == 0:
		err = fmt.Errorf("diff rs requires <deployment-name>, -l <label>, or two RS names")
	case selector == "" && len(rest) == 1:
		depName = rest[0]
	case selector == "" && len(rest) == 3:
		depName = rest[0]
		revFrom, revTo, err = parseRevPair(rest[1], rest[2])
		hasRevs = err == nil
	case selector != "" && len(rest) == 0:
		// default last two, deployment via selector
	case selector != "" && len(rest) == 2:
		revFrom, revTo, err = parseRevPair(rest[0], rest[1])
		hasRevs = err == nil
	case selector != "" && len(rest) > 0 && len(rest) != 2:
		err = fmt.Errorf("with -l/--label, expected 0 or 2 positional args (<rev-from> <rev-to>), got %d", len(rest))
	default:
		err = fmt.Errorf("expected <deployment-name> [<rev-from> <rev-to>] or two RS names, got %d args", len(rest))
	}
	return
}

func parseRevPair(a, b string) (int, int, error) {
	av, err := strconv.Atoi(a)
	if err != nil || av < 1 {
		return 0, 0, fmt.Errorf("invalid revision %q (must be a positive integer)", a)
	}
	bv, err := strconv.Atoi(b)
	if err != nil || bv < 1 {
		return 0, 0, fmt.Errorf("invalid revision %q (must be a positive integer)", b)
	}
	return av, bv, nil
}

func findByRevision(items []appsv1.ReplicaSet, rev int) *appsv1.ReplicaSet {
	target := strconv.Itoa(rev)
	for i := range items {
		if items[i].Annotations[revisionAnnotation] == target {
			return &items[i]
		}
	}
	return nil
}

func printAvailableRevisions(w io.Writer, items []appsv1.ReplicaSet) {
	fmt.Fprintln(w, "\nAvailable revisions:")
	for _, r := range items {
		rev := r.Annotations[revisionAnnotation]
		if rev == "" {
			continue
		}
		fmt.Fprintf(w, "  revision %s (%s)\n", rev, r.Name)
	}
}
