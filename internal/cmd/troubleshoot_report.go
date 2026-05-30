package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// troubleshoot_report.go holds the diagnostic engine behind the
// `kdiag troubleshoot <kind>` command (see troubleshoot.go for the entry point).
// It is kind-aware: for a pod it explains scheduling (when unscheduled) or
// runtime health (when scheduled); for a workload it reports replica health and
// drills into each unhealthy managed pod; for a node it reports node-level
// health. The pure builders (buildPodTroubleshoot, classifyWorkloadPods,
// buildNodeTroubleshoot) are unit-tested; the thin I/O wrappers are covered by
// integration tests.

// ── report shapes (also the --yaml payloads) ────────────────────────────────

type podTroubleshoot struct {
	Pod        string          `json:"pod"`
	Namespace  string          `json:"namespace"`
	Node       string          `json:"node,omitempty"`
	Phase      string          `json:"phase"`
	Verdict    string          `json:"verdict"` // Healthy / Unhealthy / Unschedulable
	Scheduling *scheduleReport `json:"scheduling,omitempty"`
	Issues     []kube.PodIssue `json:"issues,omitempty"`
	Warnings   []string        `json:"warnings,omitempty"`
}

type workloadTroubleshoot struct {
	Kind          string            `json:"kind"`
	Name          string            `json:"name"`
	Namespace     string            `json:"namespace"`
	Replicas      string            `json:"replicas,omitempty"`
	Verdict       string            `json:"verdict"` // Healthy / Degraded
	HealthyPods   int               `json:"healthyPods"`
	UnhealthyPods []podTroubleshoot `json:"unhealthyPods,omitempty"`
}

type nodeTroubleshoot struct {
	Node    string   `json:"node"`
	Verdict string   `json:"verdict"` // Healthy / Unhealthy
	Issues  []string `json:"issues,omitempty"`
	Taints  []string `json:"taints,omitempty"`
}

// ── pure builders ───────────────────────────────────────────────────────────

// buildPodTroubleshoot assembles a pod's report. Unscheduled pods carry the
// scheduling report (built by the caller); scheduled pods carry runtime issues
// and recent warnings. Pure — no client calls.
func buildPodTroubleshoot(pod corev1.Pod, sched *scheduleReport, warnings []string) podTroubleshoot {
	rep := podTroubleshoot{
		Pod:       pod.Name,
		Namespace: pod.Namespace,
		Node:      pod.Spec.NodeName,
		Phase:     string(pod.Status.Phase),
	}
	if pod.Spec.NodeName == "" {
		rep.Verdict = "Unschedulable"
		rep.Scheduling = sched
		return rep
	}
	rep.Issues = kube.PodRuntimeIssues(&pod)
	rep.Warnings = warnings
	if len(rep.Issues) == 0 {
		rep.Verdict = "Healthy"
	} else {
		rep.Verdict = "Unhealthy"
	}
	return rep
}

// podIsUnhealthy reports whether a pod needs attention: unscheduled, or with at
// least one runtime issue (which includes running-but-not-ready).
func podIsUnhealthy(pod corev1.Pod) bool {
	return pod.Spec.NodeName == "" || len(kube.PodRuntimeIssues(&pod)) > 0
}

// classifyWorkloadPods splits a workload's pods into a healthy count and the
// list of pods needing attention. Pure.
func classifyWorkloadPods(pods []corev1.Pod) (healthy int, unhealthy []corev1.Pod) {
	for i := range pods {
		if podIsUnhealthy(pods[i]) {
			unhealthy = append(unhealthy, pods[i])
		} else {
			healthy++
		}
	}
	return healthy, unhealthy
}

// buildNodeTroubleshoot assembles a node's health report. Pure.
func buildNodeTroubleshoot(n corev1.Node) nodeTroubleshoot {
	issues := kube.NodeIssues(&n)
	taints := make([]string, 0, len(n.Spec.Taints))
	for _, t := range n.Spec.Taints {
		taints = append(taints, fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect))
	}
	verdict := "Healthy"
	if len(issues) > 0 {
		verdict = "Unhealthy"
	}
	return nodeTroubleshoot{Node: n.Name, Verdict: verdict, Issues: issues, Taints: taints}
}

// ── renderers ───────────────────────────────────────────────────────────────

func printPodTroubleshoot(w io.Writer, rep podTroubleshoot) {
	// Unscheduled → the scheduling report is the whole story.
	if rep.Scheduling != nil {
		printScheduleBlock(w, *rep.Scheduling)
		return
	}
	fmt.Fprintf(w, "Pod: %s  (%s, node %s) — %s\n", rep.Pod, rep.Phase, rep.Node, rep.Verdict)
	if len(rep.Issues) == 0 {
		fmt.Fprintln(w, "  No problems detected.")
		return
	}
	fmt.Fprintln(w, "Issues:")
	for _, is := range rep.Issues {
		loc := ""
		if is.Container != "" {
			loc = "[" + is.Container + "] "
		}
		line := "  - " + loc + is.Symptom
		if is.Detail != "" {
			line += ": " + is.Detail
		}
		fmt.Fprintln(w, line)
	}
	if len(rep.Warnings) > 0 {
		fmt.Fprintln(w, "Recent warnings:")
		for _, wn := range rep.Warnings {
			fmt.Fprintln(w, "  - "+wn)
		}
	}
}

func printNodeTroubleshoot(w io.Writer, rep nodeTroubleshoot) {
	fmt.Fprintf(w, "Node: %s — %s\n", rep.Node, rep.Verdict)
	if len(rep.Issues) == 0 {
		fmt.Fprintln(w, "  No problems detected.")
	} else {
		fmt.Fprintln(w, "Issues:")
		for _, is := range rep.Issues {
			fmt.Fprintln(w, "  - "+is)
		}
	}
	if len(rep.Taints) > 0 {
		fmt.Fprintln(w, "Taints (restrict scheduling):")
		for _, t := range rep.Taints {
			fmt.Fprintln(w, "  - "+t)
		}
	}
}

func printWorkloadTroubleshoot(w io.Writer, rep workloadTroubleshoot) {
	fmt.Fprintf(w, "%s: %s — %s\n", rep.Kind, rep.Name, rep.Verdict)
	if rep.Replicas != "" {
		fmt.Fprintf(w, "  Replicas: %s\n", rep.Replicas)
	}
	fmt.Fprintf(w, "  Pods: %d healthy, %d need attention\n", rep.HealthyPods, len(rep.UnhealthyPods))
	if len(rep.UnhealthyPods) == 0 {
		fmt.Fprintln(w, "  All pods healthy.")
		return
	}
	for _, up := range rep.UnhealthyPods {
		fmt.Fprintln(w, "==========================================")
		printPodTroubleshoot(w, up)
	}
}

// ── collection (I/O) ────────────────────────────────────────────────────────

// troubleshootPod builds one pod's report, performing the I/O its mode needs:
// a scheduling report when unscheduled, recent warnings when scheduled.
func troubleshootPod(env *kube.KubeEnv, ctx context.Context, pod corev1.Pod) podTroubleshoot {
	if pod.Spec.NodeName == "" {
		sched := scheduleReportFor(env, ctx, pod)
		return buildPodTroubleshoot(pod, &sched, nil)
	}
	return buildPodTroubleshoot(pod, nil, warningEventsFor(env, ctx, pod))
}

// collectPodReports resolves the target pods and builds each one's report.
func collectPodReports(env *kube.KubeEnv, ctx context.Context, name, selector string) []podTroubleshoot {
	pods := resolvePodsForTroubleshoot(env, ctx, name, selector)
	out := make([]podTroubleshoot, 0, len(pods))
	for i := range pods {
		out = append(out, troubleshootPod(env, ctx, pods[i]))
	}
	return out
}

// renderPodReports prints pod reports as text, or emits YAML. A single named
// target emits as a scalar document; everything else emits as a list.
func renderPodReports(reps []podTroubleshoot, name string, asYAML bool) {
	if asYAML {
		if len(reps) == 1 && name != "" {
			emit(reps[0])
		} else {
			emit(reps)
		}
		return
	}
	for i := range reps {
		if i > 0 {
			fmt.Println("==========================================")
		}
		printPodTroubleshoot(os.Stdout, reps[i])
	}
}

// resolvePodsForTroubleshoot mirrors `inspect pod` resolution: a partial name
// must match exactly one pod; a selector returns all matches; neither returns
// every pod in the namespace.
func resolvePodsForTroubleshoot(env *kube.KubeEnv, ctx context.Context, name, selector string) []corev1.Pod {
	if name != "" {
		all, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(""))
		if err != nil {
			cli.Fatal(fmt.Errorf("list pods: %w", err))
		}
		var matches []corev1.Pod
		for _, p := range all.Items {
			if strings.Contains(p.Name, name) {
				matches = append(matches, p)
			}
		}
		switch len(matches) {
		case 0:
			cli.Fatal(fmt.Errorf("no pod found matching %q", name))
		case 1:
			return matches
		default:
			names := make([]string, 0, len(matches))
			for _, p := range matches {
				names = append(names, p.Name)
			}
			cli.Fatal(fmt.Errorf("%d pods match %q — be more specific: %s", len(matches), name, strings.Join(names, ", ")))
		}
	}
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(selector))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}
	if len(pods.Items) == 0 {
		cli.Fatal(fmt.Errorf("no pods found"))
	}
	return pods.Items
}

// warningEventsFor returns recent (last hour) Warning events for the pod,
// formatted "Reason: message (age)", oldest first.
func warningEventsFor(env *kube.KubeEnv, ctx context.Context, pod corev1.Pod) []string {
	evList, err := env.Clientset.CoreV1().Events(pod.Namespace).List(ctx, kube.ListOptions(""))
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-time.Hour)
	type row struct {
		ts   time.Time
		text string
	}
	var rows []row
	for _, ev := range evList.Items {
		if ev.Type != corev1.EventTypeWarning ||
			ev.InvolvedObject.Kind != "Pod" || ev.InvolvedObject.Name != pod.Name {
			continue
		}
		ts := kube.EffectiveEventTime(ev)
		if ts.IsZero() || ts.Before(cutoff) {
			continue
		}
		msg := eventMessageReplacer.Replace(ev.Message)
		rows = append(rows, row{ts: ts, text: fmt.Sprintf("%s: %s (%s)", ev.Reason, msg, kube.FormatAge(ts, time.Now()))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ts.Before(rows[j].ts) })
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.text)
	}
	return out
}

// troubleshootWorkload reports replica health and drills into each unhealthy
// managed pod.
func troubleshootWorkload(env *kube.KubeEnv, ctx context.Context, canonicalKind, name string) workloadTroubleshoot {
	label, selector, replicas := workloadForTroubleshoot(ctx, env, canonicalKind, name)
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(metav1.FormatLabelSelector(selector)))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}
	healthy, unhealthy := classifyWorkloadPods(pods.Items)
	reps := make([]podTroubleshoot, 0, len(unhealthy))
	for i := range unhealthy {
		reps = append(reps, troubleshootPod(env, ctx, unhealthy[i]))
	}
	verdict := "Healthy"
	if len(unhealthy) > 0 {
		verdict = "Degraded"
	}
	return workloadTroubleshoot{
		Kind:          label,
		Name:          name,
		Namespace:     env.Namespace,
		Replicas:      replicas,
		Verdict:       verdict,
		HealthyPods:   healthy,
		UnhealthyPods: reps,
	}
}

// workloadForTroubleshoot fetches the workload and returns its display label,
// pod selector, and replica summary string. Reuses the existing per-kind
// summary builders.
func workloadForTroubleshoot(ctx context.Context, env *kube.KubeEnv, canonical, name string) (string, *metav1.LabelSelector, string) {
	switch canonical {
	case "deployment":
		d, err := env.Clientset.AppsV1().Deployments(env.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			cli.Fatal(fmt.Errorf("get deployment: %w", err))
		}
		return "Deployment", d.Spec.Selector, deploySummary(d)["Replicas"]
	case "daemonset":
		sum, sel, err := workloadSummary(ctx, env, "ds", name)
		if err != nil {
			cli.Fatal(err)
		}
		return "DaemonSet", sel, sum["Replicas"]
	case "statefulset":
		sum, sel, err := workloadSummary(ctx, env, "sts", name)
		if err != nil {
			cli.Fatal(err)
		}
		return "StatefulSet", sel, sum["Replicas"]
	case "replicaset":
		sum, sel, err := workloadSummary(ctx, env, "rs", name)
		if err != nil {
			cli.Fatal(err)
		}
		return "ReplicaSet", sel, sum["Replicas"]
	default:
		cli.Fatal(fmt.Errorf("internal: unknown workload kind %q", canonical))
		return "", nil, ""
	}
}

// collectNodeReports resolves the target nodes and builds each one's report.
func collectNodeReports(env *kube.KubeEnv, ctx context.Context, name, selector string) []nodeTroubleshoot {
	var nodes []corev1.Node
	if name != "" {
		n, err := env.Clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			cli.Fatal(fmt.Errorf("get node: %w", err))
		}
		nodes = []corev1.Node{*n}
	} else {
		list, err := env.Clientset.CoreV1().Nodes().List(ctx, kube.ListOptions(selector))
		if err != nil {
			cli.Fatal(fmt.Errorf("list nodes: %w", err))
		}
		nodes = list.Items
	}
	out := make([]nodeTroubleshoot, 0, len(nodes))
	for i := range nodes {
		out = append(out, buildNodeTroubleshoot(nodes[i]))
	}
	return out
}

// renderNodeReports prints node reports as text, or emits YAML (scalar for a
// single named target, list otherwise).
func renderNodeReports(reps []nodeTroubleshoot, name string, asYAML bool) {
	if asYAML {
		if len(reps) == 1 && name != "" {
			emit(reps[0])
		} else {
			emit(reps)
		}
		return
	}
	for i := range reps {
		if i > 0 {
			fmt.Println("==========================================")
		}
		printNodeTroubleshoot(os.Stdout, reps[i])
	}
}
