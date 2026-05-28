//go:build integration

// Integration tests require a running Kubernetes cluster and the KUBECONFIG
// environment variable pointing to it.
//
// Quick start with kind:
//
//	make cluster-up
//	make integration-tests
//	make cluster-down
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

var binaryPath string

func TestMain(m *testing.M) {
	if os.Getenv("KUBECONFIG") == "" {
		fmt.Fprintln(os.Stderr, "KUBECONFIG is not set — skipping integration tests")
		os.Exit(0)
	}

	// Build the binary from the module root (one level above test/).
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "getwd:", err)
		os.Exit(1)
	}
	moduleRoot := filepath.Dir(wd)

	tmp, err := os.MkdirTemp("", "kdiag-integration-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdirtemp:", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)

	binaryPath = filepath.Join(tmp, "kdiag")
	// Stamp ldflags so --version reports meaningful values. Var names match
	// the verbatim go_version_template (lowercase, unexported).
	buildTime := time.Now().UTC().Format("02-01-06_15:04")
	ldflags := fmt.Sprintf(
		"-X main.version=integration -X main.buildTime=%s -X main.commit=test",
		buildTime,
	)
	build := exec.Command("go", "build", "-ldflags", ldflags, "-o", binaryPath, ".")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// run executes the kdiag binary with the given args and returns
// stdout, stderr, and the exit code.
func run(args ...string) (stdout, stderr string, code int) {
	cmd := exec.Command(binaryPath, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), code
}

// ── inspect pod ──────────────────────────────────────────────────────────────

// All pods in namespace (no pod name, no selector).
func TestInspectPod_AllPods(t *testing.T) {
	out, _, code := run("inspect", "pod", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "Container:") {
		t.Errorf("expected at least one container in output:\n%s", out)
	}
}

// Flag before subcommand: `inspect --az deploy <name>` must dispatch correctly.
func TestInspect_FlagBeforeKind(t *testing.T) {
	out, _, code := run("inspect", "--az", "deploy", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"Deployment: test-app", "POD", "NODE", "ZONE"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// -n before subcommand: `inspect -n <ns> pod <name>`.
func TestInspect_NamespaceBeforeKind(t *testing.T) {
	out, _, code := run("inspect", "-n", "kdiag-test", "pod", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Pod: kdiag-static") {
		t.Errorf("expected pod header in output:\n%s", out)
	}
}

// Single pod by name — flag before pod name. Output should include the
// pod-level summary (Node/Pod IP/QoS) and container-level Image/Tag.
func TestInspectPod_ByName_FlagFirst(t *testing.T) {
	out, _, code := run("inspect", "pod", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	assertContainerInfo(t, out)
	for _, want := range []string{
		"Pod: kdiag-static",
		"Node:",
		"Pod IP:",
		"QoS:",
		"Image:",
		"Tag:",
		"Ports:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// Single pod by name — pod name before flags (kubectl-like ordering).
func TestInspectPod_ByName_PodNameFirst(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	assertContainerInfo(t, out)
}

// `inspect pod --resources` in text mode narrows output to container name and resources only.
func TestInspectPod_Resources_Text(t *testing.T) {
	out, errOut, code := run("inspect", "pod", "--resources", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", code, out, errOut)
	}

	// Output should contain the container name and its resources.
	expected := []string{
		"  Container:         nginx",
		"    Resources:",
		"      Requests:",
		"        cpu: 10m",
		"        memory: 16Mi",
		"      Limits:",
		"        memory: 32Mi",
	}
	for _, want := range expected {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}

	// Output should NOT contain other details.
	banned := []string{
		"Pod:",
		"Node:",
		"Pod IP:",
		"QoS:",
		"Image:",
		"Tag:",
		"Ports:",
		"State:",
		"Ready:",
		"Restart Count:",
	}
	for _, ban := range banned {
		if strings.Contains(out, ban) {
			t.Errorf("did not expect %q in output:\n%s", ban, out)
		}
	}
}

// `inspect pod --resources --format yaml` emits a YAML list of {name, resources} per
// container.
func TestInspectPod_Resources_YAML(t *testing.T) {
	out, _, code := run("inspect", "pod", "--resources", "-o", "yaml", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("exit=%d, out=%s", code, out)
	}
	var entries []map[string]any
	if err := yaml.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("output is not a YAML list: %v\noutput:\n%s", err, out)
	}
	if len(entries) == 0 {
		t.Fatalf("want at least one entry, got: %s", out)
	}
	if entries[0]["name"] != "nginx" {
		t.Errorf("want name=nginx, got %v", entries[0]["name"])
	}
}

// `inspect pod --resources --format yaml -l <label>` emits a YAML list of containers
// from matching pods.
func TestInspectPod_Resources_LabelList_YAML(t *testing.T) {
	out, _, code := run("inspect", "pod", "--resources", "-o", "yaml", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("exit=%d, out=%s", code, out)
	}
	var entries []map[string]any
	if err := yaml.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("output is not a YAML list: %v\noutput:\n%s", err, out)
	}
	// 2 replicas * 1 container = 2 entries
	if len(entries) != 2 {
		t.Errorf("want 2 entries (2 pods × 1 container), got %d: %s", len(entries), out)
	}
}

// --az + --format yaml emits the AZ view as a YAML doc { placements, zoneSummary }.
// --format yaml is a format flag and composes with any view selector.
func TestInspectPod_AZ_YAML(t *testing.T) {
	out, _, code := run("inspect", "pod", "--az", "-o", "yaml", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0 for --az -o yaml, got %d\nstdout: %s", code, out)
	}
	var doc struct {
		Placements []struct {
			Pod, Node, Zone string
		} `yaml:"placements"`
		ZoneSummary map[string]int `yaml:"zoneSummary"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v\nstdout: %s", err, out)
	}
	if len(doc.Placements) == 0 {
		t.Errorf("expected at least one placement entry, got 0\nstdout: %s", out)
	}
	if doc.Placements[0].Pod != "kdiag-static" {
		t.Errorf("expected first placement pod=kdiag-static, got %q", doc.Placements[0].Pod)
	}
	if len(doc.ZoneSummary) == 0 {
		t.Errorf("expected zoneSummary to be populated, got empty\nstdout: %s", out)
	}
}

// --resources is a view selector and must not compose with --az.
func TestInspectPod_Resources_NotWithAZ(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "--resources", "--az", "-n", "kdiag-test", "kdiag-static")
	if code == 0 {
		t.Error("expected non-zero exit when --resources is combined with --az")
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' in stderr:\n%s", errOut)
	}
}

func TestInspectPod_MultiContainer_TextLabels(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-multi-container", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("exit=%d, out=%s", code, out)
	}
	wants := []string{
		"Init Container:    init-perms",
		"Sidecar Container: log-shipper",
		"Container:         app",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in output:\n%s", w, out)
		}
	}
	if strings.Contains(out, "Namespace:") {
		t.Errorf("text output must not include Namespace banner; got:\n%s", out)
	}
	// Order check: init before sidecar before regular.
	iInit := strings.Index(out, "init-perms")
	iSide := strings.Index(out, "log-shipper")
	iApp := strings.Index(out, "Container:         app")
	if iInit < 0 || iSide < 0 || iApp < 0 {
		t.Fatalf("missing one of the container markers; out:\n%s", out)
	}
	if !(iInit < iSide && iSide < iApp) {
		t.Errorf("expected init → sidecar → regular; got positions init=%d sidecar=%d regular=%d", iInit, iSide, iApp)
	}
}

func TestInspectPod_MultiContainer_YAML(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-multi-container", "-o", "yaml", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("exit=%d, out=%s", code, out)
	}
	var pod map[string]any
	if err := yaml.Unmarshal([]byte(out), &pod); err != nil {
		t.Fatalf("output is not a YAML map: %v\noutput:\n%s", err, out)
	}
	containers, _ := pod["containers"].([]any)
	if len(containers) != 3 {
		t.Fatalf("want 3 containers, got %d: %s", len(containers), out)
	}
	wantNames := []string{"init-perms", "log-shipper", "app"}
	wantKinds := []string{"Init", "Sidecar", "Regular"}
	for i, want := range wantNames {
		c, _ := containers[i].(map[string]any)
		if c["name"] != want {
			t.Errorf("containers[%d].name = %v, want %v", i, c["name"], want)
		}
		if c["kind"] != wantKinds[i] {
			t.Errorf("containers[%d].kind = %v, want %v", i, c["kind"], wantKinds[i])
		}
	}
}

func TestInspectPod_LabelMatch_YAML_IsList(t *testing.T) {
	out, _, code := run("inspect", "pod", "-l", "app=test-app", "-o", "yaml", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("exit=%d, out=%s", code, out)
	}
	var list []map[string]any
	if err := yaml.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("output is not a YAML list: %v\noutput:\n%s", err, out)
	}
	if len(list) != 2 {
		t.Errorf("want 2 pods (test-app has 2 replicas), got %d", len(list))
	}
}

func TestInspectDeploy_YAML_WorkloadShape(t *testing.T) {
	out, _, code := run("inspect", "deploy", "test-app", "-o", "yaml", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("exit=%d, out=%s", code, out)
	}
	var d map[string]any
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("output is not YAML: %v\noutput:\n%s", err, out)
	}
	if d["name"] != "test-app" {
		t.Errorf("name = %v, want test-app", d["name"])
	}
	if d["kind"] != "Deployment" {
		t.Errorf("kind = %v, want Deployment", d["kind"])
	}
	pods, _ := d["pods"].([]any)
	if len(pods) != 2 {
		t.Errorf("want 2 pods, got %d", len(pods))
	}
}

// The removed --kubeconfig / --context / --find-path flags must be reported as unknown.
func TestInspect_RemovedFlags_Rejected(t *testing.T) {
	cases := [][]string{
		{"inspect", "pod", "--kubeconfig", "/tmp/x", "-n", "kdiag-test"},
		{"inspect", "pod", "--context", "ignored", "-n", "kdiag-test"},
		{"inspect", "pod", "--selector", "app=test-app", "-n", "kdiag-test"},
		{"inspect", "pod", "--find-path", "name", "-n", "kdiag-test"},
	}
	for _, args := range cases {
		_, errOut, code := run(args...)
		if code == 0 {
			t.Errorf("expected non-zero exit for %v", args)
		}
		if !strings.Contains(errOut, "unknown flag") {
			t.Errorf("expected 'unknown flag' for %v in stderr:\n%s", args, errOut)
		}
	}
}

// Selector filters to matching pods only.
func TestInspectPod_BySelector(t *testing.T) {
	out, _, code := run("inspect", "pod", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "Container:") {
		t.Errorf("expected container info in output:\n%s", out)
	}
	if strings.Contains(out, "Namespace:") {
		t.Errorf("text output must not include Namespace banner; got:\n%s", out)
	}
}

// Selector with no matches prints a clear message.
func TestInspectPod_BySelector_NoMatch(t *testing.T) {
	out, _, code := run("inspect", "pod", "-n", "kdiag-test", "-l", "app=does-not-exist")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "No pods found") {
		t.Errorf("expected 'No pods found' in output:\n%s", out)
	}
}

// Crashing pod shows waiting/terminated state (not running).
func TestInspectPod_CrashingState(t *testing.T) {
	out, _, code := run("inspect", "pod", "-n", "kdiag-test", "kdiag-crasher")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	// Pod may be in waiting (CrashLoopBackOff) or terminated — either is valid.
	hasWaiting := strings.Contains(out, "waiting")
	hasTerminated := strings.Contains(out, "terminated")
	if !hasWaiting && !hasTerminated {
		t.Errorf("expected waiting or terminated state for crasher pod:\n%s", out)
	}
}

// Non-existent pod name exits non-zero.
func TestInspectPod_NotFound(t *testing.T) {
	_, _, code := run("inspect", "pod", "-n", "kdiag-test", "pod-does-not-exist")
	if code == 0 {
		t.Error("expected non-zero exit for missing pod")
	}
}

// Partial name matching 2+ pods errors with a disambiguation list.
// `test-app` is the substring of both test-app-<hash1> and test-app-<hash2>
// (the deployment has replicas: 2).
func TestInspectPod_AmbiguousPartialName(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "-n", "kdiag-test", "test-app")
	if code == 0 {
		t.Error("expected non-zero exit when partial name matches multiple pods")
	}
	if !strings.Contains(errOut, "pods match") {
		t.Errorf("expected 'pods match' in stderr:\n%s", errOut)
	}
	if !strings.Contains(errOut, "be more specific") {
		t.Errorf("expected 'be more specific' hint in stderr:\n%s", errOut)
	}
}

// Providing both pod name and selector is an error.
func TestInspectPod_NameAndSelector_Error(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-l", "app=static", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when both name and selector are given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// Missing subcommand (just `inspect`) exits non-zero.
func TestInspect_MissingSubcommand(t *testing.T) {
	_, errOut, code := run("inspect")
	if code == 0 {
		t.Error("expected non-zero exit for missing subcommand")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// --deployment-spec is only valid for deploy. Other kinds must error.
func TestInspect_SpecOnNonDeploy_Errors(t *testing.T) {
	for _, args := range [][]string{
		{"inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--deployment-spec"},
		{"inspect", "ds", "kdiag-ds", "-n", "kdiag-test", "--deployment-spec"},
		{"inspect", "sts", "kdiag-sts", "-n", "kdiag-test", "--deployment-spec"},
	} {
		_, errOut, code := run(args...)
		if code == 0 {
			t.Errorf("%v: want non-zero exit, got 0", args)
		}
		if !strings.Contains(errOut, "--deployment-spec") || !strings.Contains(errOut, "deploy") {
			t.Errorf("%v: want stderr mentioning --deployment-spec/deploy, got: %s", args, errOut)
		}
	}
}

// ── inspect flag-combination matrix ─────────────────────────────────────────
//
// Sweeps every pair of flags advertised by `kdiag inspect --help`. The model:
//   - View selectors (mutex): --resources, --spec, --az, --path.
//     Default (no flag) is its own view.
//   - Format flag (orthogonal): --format yaml.
//
// Every compose-pair must exit 0; every mutex-pair must exit non-zero with a
// recognisable error. If a row here is wrong, --help is lying to the user —
// either the help advertises an impossible combo or the code rejects a valid
// one. Either way, that's the bug to fix.

func TestInspectPod_FlagMatrix(t *testing.T) {
	type row struct {
		name      string
		args      []string
		wantOK    bool
		wantInErr string // substring required when wantOK is false
	}
	base := []string{"inspect", "pod", "kdiag-static", "-n", "kdiag-test"}
	rows := []row{
		{"resources+yaml composes", append(base, "--resources", "-o", "yaml"), true, ""},
		{"az+yaml composes", append(base, "--az", "-o", "yaml"), true, ""},
		{"resources+az is mutex", append(base, "--resources", "--az"), false, "mutually exclusive"},
		{"yml-path rejects --format yaml alongside", append(base, "--path", "name", "-o", "yaml"), false, "mutually exclusive"},
		{"yml-path rejects --az alongside", append(base, "--path", "name", "--az"), false, "mutually exclusive"},
		{"yml-path rejects --resources alongside", append(base, "--path", "name", "--resources"), false, "mutually exclusive"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			out, errOut, code := run(r.args...)
			if r.wantOK && code != 0 {
				t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", code, out, errOut)
			}
			if !r.wantOK && code == 0 {
				t.Fatalf("expected non-zero exit, got 0\nstdout: %s", out)
			}
			if !r.wantOK && !strings.Contains(errOut, r.wantInErr) {
				t.Errorf("expected stderr to contain %q, got: %s", r.wantInErr, errOut)
			}
		})
	}
}

// The pod subcommand's help text is dispatcher-adjacent: --path is
// registered on the parent `inspect` command, so `fs.FlagUsages()` inside
// `printInspectPodHelp` cannot list it. The prose around the Flags block is
// the only place users learn that --path exists and that --format yaml does
// not compose with it. This test locks that prose down so it cannot drift
// out of sync with the dispatcher's actual rejection behaviour pinned in
// TestInspectPod_FlagMatrix above.
func TestInspectPod_HelpMentionsYMLPath(t *testing.T) {
	out, _, code := run("inspect", "pod", "-h")
	if code != 0 {
		t.Fatalf("expected exit 0 for -h, got %d\nstderr: %s", code, out)
	}
	if !strings.Contains(out, "--path") {
		t.Errorf("expected `--path` to be mentioned in inspect pod help:\n%s", out)
	}
	// The old wording promised universal composition with -o/--output, but
	// --path rejects -o/--output. Either claim must qualify the exception
	// or be removed; the unqualified sentence is what we forbid.
	bad := "-o/--output is a format flag and composes with any view."
	if strings.Contains(out, bad) {
		t.Errorf("help still contains misleading sentence %q — it contradicts the --path rejection:\n%s", bad, out)
	}
}

// Coverage backfill: the label-selector + format branches in inspect_pod.go
// (the multi-pod YAML/resources paths) had no integration coverage. These
// tests lock them in so a future refactor of the dispatch switch cannot
// silently drop a list-output mode.
func TestInspectPod_LabelSelectorYAML(t *testing.T) {
	out, _, code := run("inspect", "pod", "-l", "app=test-app", "-n", "kdiag-test", "-o", "yaml")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", code, out)
	}
	// Multi-pod YAML is a flat list of pod-info maps. Keys are sorted
	// alphabetically by sigs.k8s.io/yaml, so we assert on shape (list
	// markers + both pod names) rather than a specific first key.
	if !strings.HasPrefix(out, "- ") {
		t.Errorf("expected output to start with YAML list marker `- `, got:\n%s", out)
	}
	if strings.Count(out, "test-app-") < 2 {
		t.Errorf("expected both test-app replicas in YAML list, got:\n%s", out)
	}
}

func TestInspectPod_LabelSelectorResourcesYAML(t *testing.T) {
	out, _, code := run("inspect", "pod", "-l", "app=test-app", "-n", "kdiag-test", "--resources", "-o", "yaml")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", code, out)
	}
	// Per-container resource slice carries `resources:` and `kind:` fields.
	if !strings.Contains(out, "resources:") || !strings.Contains(out, "kind:") {
		t.Errorf("expected per-container resource slice YAML, got:\n%s", out)
	}
}

func TestInspectPod_LabelSelectorResourcesText(t *testing.T) {
	out, _, code := run("inspect", "pod", "-l", "app=test-app", "-n", "kdiag-test", "--resources")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", code, out)
	}
	// Multi-pod text mode prints a `==========` separator between pods.
	if !strings.Contains(out, "==========") {
		t.Errorf("expected `==========` separator between pods, got:\n%s", out)
	}
}

// Argument-validation gates — these branches in inspect_pod.go had no
// integration coverage even though their error messages are user-facing.
func TestInspectPod_NameAndSelectorRejected(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-l", "app=test-app", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when both pod name and -l are given")
	}
	if !strings.Contains(errOut, "provide either") {
		t.Errorf("expected `provide either` in stderr, got:\n%s", errOut)
	}
}

func TestInspectPod_TwoNamesRejected(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "test-app", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when two positional names are given")
	}
	if !strings.Contains(errOut, "only one") {
		t.Errorf("expected `only one` in stderr, got:\n%s", errOut)
	}
}

func TestInspectPod_NoMatch(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "ZZZ-no-such-pod-ZZZ", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when no pod matches")
	}
	if !strings.Contains(errOut, "no pod found") {
		t.Errorf("expected `no pod found` in stderr, got:\n%s", errOut)
	}
}

// Partial `test-app` matches both replicas of the test-app deployment, so
// the disambiguation branch fires and enumerates the candidate pod names.
func TestInspectPod_MultiMatchDisambiguates(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "test-app", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when partial name matches multiple pods")
	}
	if !strings.Contains(errOut, "be more specific") {
		t.Errorf("expected `be more specific` in stderr, got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "test-app-") {
		t.Errorf("expected candidate pod names in stderr, got:\n%s", errOut)
	}
}

// Same matrix at the workload layer (deploy adds --spec). Locks down the
// view-vs-format model across the per-kind handlers, which historically had
// inconsistent rejections.
func TestInspectDeploy_FlagMatrix(t *testing.T) {
	type row struct {
		name      string
		args      []string
		wantOK    bool
		wantInErr string
	}
	base := []string{"inspect", "deploy", "test-app", "-n", "kdiag-test"}
	rows := []row{
		{"resources+yaml composes", append(base, "--resources", "-o", "yaml"), true, ""},
		{"spec+yaml composes", append(base, "--deployment-spec", "-o", "yaml"), true, ""},
		{"az+yaml composes", append(base, "--az", "-o", "yaml"), true, ""},
		{"resources+spec is mutex", append(base, "--resources", "--deployment-spec"), false, "mutually exclusive"},
		{"resources+az is mutex", append(base, "--resources", "--az"), false, "mutually exclusive"},
		{"spec+az is mutex", append(base, "--deployment-spec", "--az"), false, "mutually exclusive"},
		{"path+spec is mutex", append(base, "--path", "memory", "--deployment-spec"), false, "mutually exclusive"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			out, errOut, code := run(r.args...)
			if r.wantOK && code != 0 {
				t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", code, out, errOut)
			}
			if !r.wantOK && code == 0 {
				t.Fatalf("expected non-zero exit, got 0\nstdout: %s", out)
			}
			if !r.wantOK && !strings.Contains(errOut, r.wantInErr) {
				t.Errorf("expected stderr to contain %q, got: %s", r.wantInErr, errOut)
			}
		})
	}
}

// -o/--output value validation: must be json or yaml; anything else errors.
func TestInspect_Output_InvalidValue(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "-o", "text")
	if code == 0 {
		t.Fatal("expected non-zero exit for -o text")
	}
	if !strings.Contains(errOut, "json") || !strings.Contains(errOut, "yaml") {
		t.Errorf("expected stderr to mention json/yaml, got: %s", errOut)
	}
}

// `-o json` for the pod info path emits parseable JSON.
func TestInspectPod_JSON(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "-o", "json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", code, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if got["name"] != "kdiag-static" {
		t.Errorf("expected name=kdiag-static, got: %v", got["name"])
	}
}

// `-o json --resources` emits the per-container resource slice as a JSON list.
func TestInspectPod_Resources_JSON(t *testing.T) {
	out, _, code := run("inspect", "pod", "--resources", "-o", "json", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", code, out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got) == 0 {
		t.Errorf("expected at least one container entry, got: %s", out)
	}
}

// `-o json` works for `inspect node` too.
func TestInspectNode_JSON(t *testing.T) {
	out, _, code := run("inspect", "node", "-o", "json")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\nstdout: %s", code, out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got) == 0 {
		t.Errorf("expected at least one node entry, got: %s", out)
	}
}

// ── inspect deploy / ds / sts / rs ────────────────────────────────────────────

// `inspect deploy` shows the workload summary plus container blocks for every
// pod in the deployment.
func TestInspectDeploy_ByName(t *testing.T) {
	out, _, code := run("inspect", "deploy", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{
		"Deployment: test-app",
		"Replicas:",
		"Selector:",
		"Strategy:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	assertContainerInfo(t, out)
}

// `inspect deploy -l <label>` resolves the deployment via label (mirrors
// diff rs) and prints the same summary as the positional form.
func TestInspectDeploy_ByLabel(t *testing.T) {
	out, _, code := run("inspect", "deploy", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Deployment: test-app") {
		t.Errorf("expected 'Deployment: test-app' in output:\n%s", out)
	}
}

// Providing both <name> and --label is an error on inspect deploy.
func TestInspectDeploy_NameAndLabel_Error(t *testing.T) {
	_, errOut, code := run("inspect", "deploy", "test-app", "-l", "app=test-app", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when both name and --label are given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// --label matching zero deployments errors out.
func TestInspectDeploy_LabelNoMatch_Error(t *testing.T) {
	_, errOut, code := run("inspect", "deploy", "-n", "kdiag-test", "-l", "app=does-not-exist")
	if code == 0 {
		t.Error("expected non-zero exit when --label matches no deployment")
	}
	if !strings.Contains(errOut, "no deployments matched") {
		t.Errorf("expected 'no deployments matched' in stderr:\n%s", errOut)
	}
}

// --label matching 2+ deployments errors with a disambiguation list. The
// fixture defines kdiag-multi-a and kdiag-multi-b both labeled
// `kdiag-multi=yes` for this case.
func TestInspectDeploy_LabelMultiMatch_Error(t *testing.T) {
	_, errOut, code := run("inspect", "deploy", "-n", "kdiag-test", "-l", "kdiag-multi=yes")
	if code == 0 {
		t.Error("expected non-zero exit when --label matches multiple deployments")
	}
	if !strings.Contains(errOut, "matched 2 deployments") {
		t.Errorf("expected 'matched 2 deployments' in stderr:\n%s", errOut)
	}
	for _, want := range []string{"kdiag-multi-a", "kdiag-multi-b"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("expected disambiguation entry %q in stderr:\n%s", want, errOut)
		}
	}
}

// `inspect deploy --resources --format yaml` emits a flat list of {name, kind, resources}
// across all pods belonging to the deployment. Should NOT iterate pods or show "Pod:" /
// "Container:" headers from the text mode.
func TestInspectDeploy_Resources_YAML(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--resources", "-o", "yaml", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"name: nginx", "kind: Regular", "resources:", "requests:", "limits:", "cpu: 50m", "memory: 32Mi"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in YAML output:\n%s", want, out)
		}
	}
	for _, banned := range []string{"Pod:", "Container:", "Deployment: test-app"} {
		if strings.Contains(out, banned) {
			t.Errorf("YAML mode should not include text header %q:\n%s", banned, out)
		}
	}
}

// `inspect deploy --spec --format yaml` emits .spec.template.spec as YAML. Keys are
// alphabetized by sigs.k8s.io/yaml (JSON marshalling), so "image" precedes
// "name" inside each container entry — assert the keys separately, not as
// a sequence-marker line.
func TestInspectDeploy_Spec_YAML(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--deployment-spec", "-o", "yaml", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"containers:", "name: nginx", "image: nginx:alpine"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in YAML output:\n%s", want, out)
		}
	}
}

// `inspect deploy --deployment-spec` (text mode) emits only the per-container
// blocks from the deployment's pod template. No "Deployment: ... (template)"
// header, and no Pod/Node/Pod IP/QoS preamble — a template has no scheduled
// pod, so those fields would only be dashes. Regression test for #24.
func TestInspectDeploy_Spec_Text(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--deployment-spec", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, banned := range []string{"(template)", "Pod:", "Node:", "Pod IP:", "QoS:"} {
		if strings.Contains(out, banned) {
			t.Errorf("text --deployment-spec should not include %q:\n%s", banned, out)
		}
	}
	for _, want := range []string{"Container:", "Image:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in text --deployment-spec output:\n%s", want, out)
		}
	}
}

// --az + --format yaml emits the AZ view as a YAML doc on a deployment too.
func TestInspectDeploy_AZ_YAML(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--az", "-o", "yaml", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0 for --az -o yaml on deploy, got %d\nstdout: %s", code, out)
	}
	if !strings.Contains(out, "placements:") || !strings.Contains(out, "zoneSummary:") {
		t.Errorf("expected `placements:` and `zoneSummary:` in YAML output:\n%s", out)
	}
}

// `inspect ds` prints the daemonset summary plus container blocks per pod.
func TestInspectDS_ByName(t *testing.T) {
	out, _, code := run("inspect", "ds", "-n", "kdiag-test", "kdiag-ds")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{
		"DaemonSet: kdiag-ds",
		"Replicas:",
		"Selector:",
		"Update Strategy:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	assertContainerInfo(t, out)
}

// `inspect daemonset` (canonical spelling) is equivalent to `ds`.
func TestInspectDS_Alias(t *testing.T) {
	out, _, code := run("inspect", "daemonset", "-n", "kdiag-test", "kdiag-ds")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "DaemonSet: kdiag-ds") {
		t.Errorf("expected DaemonSet header in output:\n%s", out)
	}
}

// `inspect sts` prints the statefulset summary plus container blocks per pod.
func TestInspectSTS_ByName(t *testing.T) {
	out, _, code := run("inspect", "sts", "-n", "kdiag-test", "kdiag-sts")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{
		"StatefulSet: kdiag-sts",
		"Replicas:",
		"Service Name:",
		"Selector:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	assertContainerInfo(t, out)
}

// `inspect statefulset` (canonical spelling) is equivalent to `sts`.
func TestInspectSTS_Alias(t *testing.T) {
	_, _, code := run("inspect", "statefulset", "-n", "kdiag-test", "kdiag-sts")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

// `inspect rs` against a non-existent name must exit non-zero — exercises the
// dispatch + handler chain.
func TestInspectRS_NotFound(t *testing.T) {
	_, errOut, code := run("inspect", "rs", "-n", "kdiag-test", "no-such-rs")
	if code == 0 {
		t.Error("expected non-zero exit for missing replicaset")
	}
	if !strings.Contains(errOut, "replicaset") {
		t.Errorf("expected error message mentioning replicaset:\n%s", errOut)
	}
}

// `inspect rs <name>` happy path: resolve a real ReplicaSet name for the
// fixture deployment via kubectl, then verify the workload summary +
// per-pod container blocks render. Covers the success path of
// runWorkload("rs", ...) which TestInspectRS_NotFound only exercises on the
// error branch.
func TestInspectRS_ByName(t *testing.T) {
	rsName := firstReplicaSetName(t, "kdiag-test", "app=test-app")
	out, _, code := run("inspect", "rs", "-n", "kdiag-test", rsName)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{
		"ReplicaSet: " + rsName,
		"Replicas:",
		"Selector:",
		"Owner:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// firstReplicaSetName shells to kubectl to fetch a ReplicaSet name matching
// the given selector. Used to test handlers that take a generated workload
// name we cannot hard-code.
func firstReplicaSetName(t *testing.T, namespace, selector string) string {
	t.Helper()
	cmd := exec.Command("kubectl", "get", "rs",
		"-n", namespace,
		"-l", selector,
		"-o", "jsonpath={.items[0].metadata.name}")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("kubectl get rs: %v", err)
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		t.Fatalf("no ReplicaSet found for -l %s in namespace %s", selector, namespace)
	}
	return name
}

// Workload kinds require exactly one positional name.
func TestInspectDeploy_MissingName(t *testing.T) {
	_, errOut, code := run("inspect", "deploy", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when name is missing")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// ── inspect node ──────────────────────────────────────────────────────────────

// `inspect node` (no args) lists every node with its full summary block.
func TestInspectNode_All(t *testing.T) {
	out, _, code := run("inspect", "node")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{
		"Node:",
		"Zone:",
		"Instance Type:",
		"Kubelet Version:",
		"Taints:",
		"Conditions:",
		"Ready:",
		"Allocatable:",
		"cpu:",
		"memory:",
		"pods:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// `inspect node <name>` finds and prints a single node when it exists.
func TestInspectNode_ByName(t *testing.T) {
	// The kind cluster's control-plane node has a deterministic name suffix.
	all, _, code := run("inspect", "node")
	if code != 0 {
		t.Fatalf("setup: list nodes failed (%d): %s", code, all)
	}
	// Pick the first "Node: <name>" line.
	var nodeName string
	for _, line := range strings.Split(all, "\n") {
		if strings.HasPrefix(line, "Node: ") {
			nodeName = strings.TrimPrefix(line, "Node: ")
			break
		}
	}
	if nodeName == "" {
		t.Fatalf("could not find a node name in:\n%s", all)
	}
	out, _, code := run("inspect", "node", nodeName)
	if code != 0 {
		t.Fatalf("expected exit 0 inspecting node %q, got %d\noutput: %s", nodeName, code, out)
	}
	if !strings.Contains(out, "Node: "+nodeName) {
		t.Errorf("expected 'Node: %s' in output:\n%s", nodeName, out)
	}
}

// `inspect node` accepts but ignores `-n` because nodes are cluster-scoped.
func TestInspectNode_NamespaceIgnored(t *testing.T) {
	out, _, code := run("inspect", "node", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0 with -n on cluster-scoped node, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Node:") {
		t.Errorf("expected nodes listed despite -n:\n%s", out)
	}
}

// `inspect no` (alias) works the same as `inspect node`.
func TestInspectNode_Alias(t *testing.T) {
	out, _, code := run("inspect", "no")
	if code != 0 {
		t.Fatalf("expected exit 0 for alias 'no', got %d", code)
	}
	if !strings.Contains(out, "Node:") {
		t.Errorf("expected node listing in output:\n%s", out)
	}
}

// `inspect node bogus` exits non-zero with a clear error.
func TestInspectNode_NotFound(t *testing.T) {
	_, errOut, code := run("inspect", "node", "definitely-not-a-real-node-12345")
	if code == 0 {
		t.Error("expected non-zero exit for missing node")
	}
	if !strings.Contains(errOut, "node") {
		t.Errorf("expected error message mentioning node:\n%s", errOut)
	}
}

// Unknown inspect kind exits non-zero with a clear error.
func TestInspect_UnknownKind(t *testing.T) {
	_, errOut, code := run("inspect", "thingamajig", "foo")
	if code == 0 {
		t.Error("expected non-zero exit for unknown kind")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// ── inspect --az ─────────────────────────────────────────────────────────────

// `inspect pod --az` shows the AZ placement table for all pods.
func TestInspectPodAZ_AllPods(t *testing.T) {
	out, _, code := run("inspect", "pod", "--az", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"POD", "NODE", "ZONE", "Summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Container:") {
		t.Errorf("--az output should not contain container state:\n%s", out)
	}
}

// `inspect pod --az -l <selector>` shows AZ for matching pods only.
func TestInspectPodAZ_BySelector(t *testing.T) {
	out, _, code := run("inspect", "pod", "--az", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"POD", "NODE", "ZONE", "Summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// `inspect deploy --az` shows AZ placement for the deployment's pods.
func TestInspectDeployAZ(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--az", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"Deployment: test-app", "POD", "NODE", "ZONE", "Summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Container:") {
		t.Errorf("--az output should not contain container state:\n%s", out)
	}
}

// `inspect ds --az` shows AZ placement for the daemonset's pods.
func TestInspectDSAZ(t *testing.T) {
	out, _, code := run("inspect", "ds", "--az", "-n", "kdiag-test", "kdiag-ds")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"DaemonSet: kdiag-ds", "POD", "NODE", "ZONE", "Summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// `inspect sts --az` shows AZ placement for the statefulset's pods.
func TestInspectSTSAZ(t *testing.T) {
	out, _, code := run("inspect", "sts", "--az", "-n", "kdiag-test", "kdiag-sts")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"StatefulSet: kdiag-sts", "POD", "NODE", "ZONE", "Summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// Short aliases: `po` works like `pod` / `pods`.
func TestInspectPo_Alias(t *testing.T) {
	out, _, code := run("inspect", "po", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0 for 'inspect po', got %d", code)
	}
	assertContainerInfo(t, out)
}

// ── diff rs ───────────────────────────────────────────────────────────────────

func TestRSDiff_ByName(t *testing.T) {
	out, _, code := run("diff", "rs", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"Deployment: kdiag-test/test-app", "Diff: revision"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestRSDiff_BySelector(t *testing.T) {
	out, _, code := run("diff", "rs", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Deployment: kdiag-test/test-app") {
		t.Errorf("expected deployment header in output:\n%s", out)
	}
}

// `diff rs --full` dumps the full RS objects via the dynamic client instead
// of just .spec.template. The unified diff only shows lines that DIFFER, so
// the marker must be an RS-metadata field whose value changes across
// revisions. `deployment.kubernetes.io/revision` (RS-level annotation set by
// the deployment controller) and `generation` (RS-level field) both differ
// across revisions and never appear in template-only mode.
func TestRSDiff_Full(t *testing.T) {
	out, _, code := run("diff", "rs", "-n", "kdiag-test", "--full", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Deployment: kdiag-test/test-app") {
		t.Errorf("expected deployment header in output:\n%s", out)
	}
	for _, want := range []string{"deployment.kubernetes.io/revision:", "generation:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in --full output:\n%s", want, out)
		}
	}

	// Sanity check: the same fields must NOT appear in template-only mode.
	plain, _, code := run("diff", "rs", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("template-only run failed: exit %d\noutput: %s", code, plain)
	}
	for _, notWant := range []string{"deployment.kubernetes.io/revision:", "generation:"} {
		if strings.Contains(plain, notWant) {
			t.Errorf("did not expect %q in template-only output (only --full should include it):\n%s", notWant, plain)
		}
	}
}

func TestDiff_MissingSubcommand(t *testing.T) {
	_, errOut, code := run("diff")
	if code == 0 {
		t.Error("expected non-zero exit for missing subcommand")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

func TestRSDiff_NoArgsNoSelector(t *testing.T) {
	_, errOut, code := run("diff", "rs", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when neither name nor selector given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

func TestRSDiff_NameAndSelector_Error(t *testing.T) {
	_, errOut, code := run("diff", "rs", "-n", "kdiag-test", "test-app", "-l", "app=test-app")
	if code == 0 {
		t.Error("expected non-zero exit when both name and selector given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// Specific revisions: `kdiag diff rs <name> <revA> <revB>` must compare
// the requested revisions instead of defaulting to the last two. The
// fixture's cluster-up creates revisions 1, 2, 3 for test-app.
func TestRSDiff_SpecificRevisions(t *testing.T) {
	out, _, code := run("diff", "rs", "-n", "kdiag-test", "test-app", "1", "3")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Diff: revision 1 → 3") {
		t.Errorf("expected 'Diff: revision 1 → 3' in output:\n%s", out)
	}
}

// Order is preserved: `... 3 1` shows "revision 3 → 1", not normalised.
func TestRSDiff_SpecificRevisions_OrderPreserved(t *testing.T) {
	out, _, code := run("diff", "rs", "-n", "kdiag-test", "test-app", "3", "1")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Diff: revision 3 → 1") {
		t.Errorf("expected 'Diff: revision 3 → 1' in output:\n%s", out)
	}
}

// Same revision twice: empty diff, exit 0 (the diff command's exit 1 when
// differences are found is handled; identical files are exit 0).
func TestRSDiff_SameRevision(t *testing.T) {
	out, _, code := run("diff", "rs", "-n", "kdiag-test", "test-app", "2", "2")
	if code != 0 {
		t.Fatalf("expected exit 0 for identical revisions, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Diff: revision 2 → 2") {
		t.Errorf("expected header in output:\n%s", out)
	}
}

// Bogus revision number: error names the missing revision and lists
// available revisions so the user can retry.
func TestRSDiff_BadRevision(t *testing.T) {
	_, errOut, code := run("diff", "rs", "-n", "kdiag-test", "test-app", "1", "99")
	if code == 0 {
		t.Error("expected non-zero exit for missing revision")
	}
	for _, want := range []string{"revision not found", "99", "Available revisions"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("expected %q in stderr:\n%s", want, errOut)
		}
	}
}

// Non-numeric revisions are rejected with a clear error before hitting the API.
func TestRSDiff_NonNumericRevision(t *testing.T) {
	_, errOut, code := run("diff", "rs", "-n", "kdiag-test", "test-app", "abc", "def")
	if code == 0 {
		t.Error("expected non-zero exit for non-numeric revision")
	}
	if !strings.Contains(errOut, "invalid revision") {
		t.Errorf("expected 'invalid revision' in stderr:\n%s", errOut)
	}
}

// Two positional args without selector route to the generic two-name
// path (compare RS objects by name). With non-existent names this must
// produce a non-zero exit and surface the missing-resource error.
func TestRSDiff_TwoArgsNoSelector_GenericPath(t *testing.T) {
	_, errOut, code := run("diff", "rs", "-n", "kdiag-test", "no-such-rs-a", "no-such-rs-b")
	if code == 0 {
		t.Error("expected non-zero exit when neither RS exists")
	}
	if !strings.Contains(errOut, "no-such-rs-a") && !strings.Contains(errOut, "not found") {
		t.Errorf("expected error mentioning the missing rs in stderr:\n%s", errOut)
	}
}

// Selector + revision-pair (no deploy name) is the recommended form when the
// deployment is identified by labels.
func TestRSDiff_BySelector_WithRevisions(t *testing.T) {
	out, _, code := run("diff", "rs", "-n", "kdiag-test", "-l", "app=test-app", "1", "3")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Diff: revision 1 → 3") {
		t.Errorf("expected 'Diff: revision 1 → 3' in output:\n%s", out)
	}
}

func TestRSDiff_NoMatch_Error(t *testing.T) {
	_, errOut, code := run("diff", "rs", "-n", "kdiag-test", "-l", "app=does-not-exist")
	if code == 0 {
		t.Error("expected non-zero exit when selector matches no deployments")
	}
	if strings.TrimSpace(errOut) == "" {
		t.Errorf("expected error message in stderr, got nothing")
	}
}

// ── diff pod ──────────────────────────────────────────────────────────────────

func TestDiffPod_TwoPods(t *testing.T) {
	out, _, code := run("diff", "pod", "-n", "kdiag-test", "kdiag-static", "kdiag-crasher")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	// Signal — must appear. The banner identifies which pods are being
	// compared; the spec-level differences (image, label) are the actual
	// content an investigator wants.
	for _, want := range []string{
		"Diff: pod/kdiag-static vs pod/kdiag-crasher",
		"image: nginx:alpine",
		"image: busybox:latest",
		"app: static",
		"app: crasher",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
	// Noise — must NOT appear. These fields differ between any two
	// distinct resources but say nothing about what's actually different
	// (etcd bookkeeping, random IDs, runtime container IDs, the
	// auto-injected projected SA-token volume, the metadata.name that the
	// banner already shows). --full is the escape hatch for users who
	// need them. `uid:` is omitted on purpose because YAML can carry
	// `runAsUser`-style `uid: 0` lines in security contexts that aren't
	// the metadata uid.
	for _, noise := range []string{
		"managedFields:",
		"resourceVersion:",
		"creationTimestamp:",
		"containerID:",
		"kubectl.kubernetes.io/last-applied-configuration",
		"kube-api-access-",
		"name: kdiag-static",
		"name: kdiag-crasher",
	} {
		if strings.Contains(out, noise) {
			t.Errorf("did not expect %q (noise) in default diff output:\n%s", noise, out)
		}
	}
}

// --full disables every Go-side massaging step — managedFields and
// every other API-server-returned field must appear verbatim.
func TestDiffPod_FullDiff_ShowsNoise(t *testing.T) {
	out, _, code := run("diff", "pod", "-n", "kdiag-test", "kdiag-static", "kdiag-crasher", "--full")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{
		"managedFields:",
		"resourceVersion:",
		"uid:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in --full output (raw mode must preserve all fields):\n%s", want, out)
		}
	}
}

// Generic dispatch: kdiag diff <any-kind> <a> <b> must work for kinds
// outside the (rs, pod, node) shortlist. ConfigMap is a useful smoke
// because it has no `spec` — so the old `.Spec`-only handler would have
// produced an empty diff.
func TestDiff_ConfigMap_AnyKind(t *testing.T) {
	out, _, code := run("diff", "cm", "-n", "kdiag-test", "kdiag-cm-a", "kdiag-cm-b")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{
		"Diff: configmap/kdiag-cm-a vs configmap/kdiag-cm-b",
		"greeting: hello",
		"greeting: hola",
		"extra: only-in-b",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestDiffPod_SamePod(t *testing.T) {
	out, _, code := run("diff", "pod", "-n", "kdiag-test", "kdiag-static", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0 for identical pods, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Diff: pod/kdiag-static vs pod/kdiag-static") {
		t.Errorf("expected diff header in output:\n%s", out)
	}
}

func TestDiffPod_MissingName(t *testing.T) {
	_, errOut, code := run("diff", "pod", "-n", "kdiag-test", "kdiag-static")
	if code == 0 {
		t.Error("expected non-zero exit when only one pod name is given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

func TestDiffPod_NotFound(t *testing.T) {
	_, errOut, code := run("diff", "pod", "-n", "kdiag-test", "kdiag-static", "no-such-pod")
	if code == 0 {
		t.Error("expected non-zero exit for missing pod")
	}
	if !strings.Contains(errOut, "pod") {
		t.Errorf("expected error mentioning pod in stderr:\n%s", errOut)
	}
}

func TestDiffPod_Alias(t *testing.T) {
	out, _, code := run("diff", "po", "-n", "kdiag-test", "kdiag-static", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0 for alias 'po', got %d", code)
	}
	if !strings.Contains(out, "Diff: pod/") {
		t.Errorf("expected diff header in output:\n%s", out)
	}
}

// ── diff node ─────────────────────────────────────────────────────────────────

func TestDiffNode_SameNode(t *testing.T) {
	all, _, code := run("inspect", "node")
	if code != 0 {
		t.Fatalf("setup: list nodes failed: %s", all)
	}
	var nodeName string
	for _, line := range strings.Split(all, "\n") {
		if strings.HasPrefix(line, "Node: ") {
			nodeName = strings.TrimPrefix(line, "Node: ")
			break
		}
	}
	if nodeName == "" {
		t.Fatalf("could not find a node name in:\n%s", all)
	}

	out, _, code := run("diff", "node", nodeName, nodeName)
	if code != 0 {
		t.Fatalf("expected exit 0 for identical nodes, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Diff: node/"+nodeName+" vs node/"+nodeName) {
		t.Errorf("expected diff header in output:\n%s", out)
	}
}

func TestDiffNode_MissingName(t *testing.T) {
	_, errOut, code := run("diff", "node", "only-one-node")
	if code == 0 {
		t.Error("expected non-zero exit when only one node name is given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

func TestDiffNode_NotFound(t *testing.T) {
	_, errOut, code := run("diff", "node", "no-such-node", "also-not-a-node")
	if code == 0 {
		t.Error("expected non-zero exit for missing node")
	}
	if !strings.Contains(errOut, "node") {
		t.Errorf("expected error mentioning node in stderr:\n%s", errOut)
	}
}

func TestDiffNode_Alias(t *testing.T) {
	all, _, code := run("inspect", "node")
	if code != 0 {
		t.Fatalf("setup: list nodes failed: %s", all)
	}
	var nodeName string
	for _, line := range strings.Split(all, "\n") {
		if strings.HasPrefix(line, "Node: ") {
			nodeName = strings.TrimPrefix(line, "Node: ")
			break
		}
	}
	if nodeName == "" {
		t.Fatalf("could not find a node name in:\n%s", all)
	}

	out, _, code := run("diff", "no", nodeName, nodeName)
	if code != 0 {
		t.Fatalf("expected exit 0 for alias 'no', got %d", code)
	}
	if !strings.Contains(out, "Diff: node/") {
		t.Errorf("expected diff header in output:\n%s", out)
	}
}

// ── events ───────────────────────────────────────────────────────────────────

// Default run: all event types in namespace. The crashing pod generates
// BackOff (Warning) events and the deployment generates Scheduled/Pulled
// (Normal) events, so we expect both type labels to appear.
func TestEvents_Default(t *testing.T) {
	out, _, code := run("events", "-n", "kdiag-test", "--since", "999h")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Warning") {
		t.Errorf("expected Warning events in output:\n%s", out)
	}
	if !strings.Contains(out, "Normal") {
		t.Errorf("expected Normal events in default (all-types) output:\n%s", out)
	}
	if !strings.Contains(out, "AGE") || !strings.Contains(out, "REASON") {
		t.Errorf("expected table header (AGE REASON) in output:\n%s", out)
	}
}

// --since 1s: all historical events are older than 1s, so kube-system returns empty.
func TestEvents_EmptyResult(t *testing.T) {
	out, _, code := run("events", "-n", "kube-system", "--since", "1s")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "No events in namespace kube-system (last 1s).") {
		t.Errorf("expected empty-result message in output:\n%s", out)
	}
}

// --since accepts seconds/minutes/hours via Go duration syntax.
func TestEvents_SinceUnits(t *testing.T) {
	for _, dur := range []string{"30s", "5m", "1h"} {
		_, _, code := run("events", "-n", "kdiag-test", "--since", dur)
		if code != 0 {
			t.Errorf("expected exit 0 for --since %s, got %d", dur, code)
		}
	}
}

// -A lists events across all namespaces. Output includes the NAMESPACE column
// and the header shows the <all> scope marker.
func TestEvents_AllNamespaces(t *testing.T) {
	out, _, code := run("events", "-A", "--since", "999h")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Namespace: <all>") {
		t.Errorf("expected '<all>' namespace header in -A output:\n%s", out)
	}
	if !strings.Contains(out, "NAMESPACE") {
		t.Errorf("expected NAMESPACE column header in -A output:\n%s", out)
	}
	if !strings.Contains(out, "kdiag-test") {
		t.Errorf("expected kdiag-test namespace to appear in -A output:\n%s", out)
	}
}

// Sort order: with multiple events, the last data row's AGE should be the
// smallest (newest entry last). FormatAge emits seconds/minutes/hours/days
// with single-letter suffix, so we sort numerically per-unit.
func TestEvents_SortNewestLast(t *testing.T) {
	out, _, code := run("events", "-n", "kdiag-test", "--since", "999h")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// Find the header line, collect AGE column from each subsequent data row.
	var ages []string
	headerSeen := false
	for _, ln := range lines {
		if !headerSeen {
			if strings.HasPrefix(strings.TrimSpace(ln), "AGE") {
				headerSeen = true
			}
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		ages = append(ages, fields[0])
	}
	if len(ages) < 2 {
		t.Skipf("need >=2 event rows to assert sort order, got %d", len(ages))
	}
	// Compare first and last age: last should be <= first when sorted newest-last.
	// Use a coarse rank: d > h > m > s.
	rank := func(a string) int {
		switch {
		case strings.HasSuffix(a, "d"):
			return 4
		case strings.HasSuffix(a, "h"):
			return 3
		case strings.HasSuffix(a, "m"):
			return 2
		case strings.HasSuffix(a, "s"):
			return 1
		}
		return 0
	}
	first, last := ages[0], ages[len(ages)-1]
	if rank(last) > rank(first) {
		t.Errorf("expected newest-last sort, but first=%q is newer than last=%q\noutput:\n%s", first, last, out)
	}
}

// Events whose Message contains literal \n or \t (probe stderr, hand-crafted
// operator events, etc.) must be flattened before reaching tabwriter — \n
// terminates the row and \t separates columns, so leaving them in would
// desync the table for every event that follows. The fixture ships a
// hand-crafted Event with reason KdiagMultilineTest and message
// "line one\nline two\twith-tab"; assert it round-trips to the rendered
// "line one line two with-tab" on a single, well-formed row.
func TestEvents_MultilineMessageSanitized(t *testing.T) {
	out, _, code := run("events", "-n", "kdiag-test", "--since", "999h")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	const reason = "KdiagMultilineTest"
	const wantMsg = "line one line two with-tab"
	var found bool
	for _, ln := range strings.Split(out, "\n") {
		if !strings.Contains(ln, reason) {
			continue
		}
		found = true
		if !strings.Contains(ln, wantMsg) {
			t.Errorf("expected sanitized message %q on the same row as reason %q, got:\n%s\n\nfull output:\n%s", wantMsg, reason, ln, out)
		}
		break
	}
	if !found {
		t.Fatalf("expected an event with reason %q in output:\n%s", reason, out)
	}
}

// -h prints usage to stdout.
func TestEvents_Help(t *testing.T) {
	out, _, code := run("events", "-h")
	if code != 0 {
		t.Fatalf("expected exit 0 for -h, got %d", code)
	}
	for _, want := range []string{"--namespace", "--since", "--all-namespaces", "Examples:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in events help output:\n%s", want, out)
		}
	}
}

// ── sort ─────────────────────────────────────────────────────────────────────

// Pod listing: header is present, the static pod fixture appears. Smoke test
// that the command runs and yields a table.
func TestSort_Pods(t *testing.T) {
	out, _, code := run("sort", "pod", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"Kind: pod", "AGE", "CREATED", "NAME", "kdiag-static"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in sort pod output:\n%s", want, out)
		}
	}
}

// Deployment alias resolves; output reports canonical "deployment" so users
// see the kind they'd query the API with.
func TestSort_DeployAlias(t *testing.T) {
	out, _, code := run("sort", "deploy", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Kind: deployment") {
		t.Errorf("expected canonical 'Kind: deployment' header:\n%s", out)
	}
	if !strings.Contains(out, "test-app") {
		t.Errorf("expected test-app in deployment sort output:\n%s", out)
	}
}

// -A inserts the NAMESPACE column and switches the scope banner.
func TestSort_AllNamespaces(t *testing.T) {
	out, _, code := run("sort", "pod", "-A")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"Namespace: <all>", "NAMESPACE", "kdiag-test"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in sort pod -A output:\n%s", want, out)
		}
	}
}

// node is cluster-scoped; -n is silently ignored and the banner reads "Scope: cluster".
func TestSort_Node(t *testing.T) {
	out, _, code := run("sort", "node", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Scope: cluster") {
		t.Errorf("expected 'Scope: cluster' for cluster-scoped kind:\n%s", out)
	}
	if !strings.Contains(out, "Kind: node") {
		t.Errorf("expected 'Kind: node' in output:\n%s", out)
	}
	// In CI we use kind, which yields at least one node.
	if !strings.Contains(out, "AGE") {
		t.Errorf("expected table header:\n%s", out)
	}
}

// Sort order: AGE values decrease (newer) toward the bottom. Uses the same
// rank trick as TestEvents_SortNewestLast.
func TestSort_NewestLast(t *testing.T) {
	out, _, code := run("sort", "pod", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var ages []string
	headerSeen := false
	for _, ln := range lines {
		if !headerSeen {
			if strings.HasPrefix(strings.TrimSpace(ln), "AGE") {
				headerSeen = true
			}
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		ages = append(ages, fields[0])
	}
	if len(ages) < 2 {
		t.Skipf("need >=2 pod rows to assert sort order, got %d", len(ages))
	}
	rank := func(a string) int {
		switch {
		case strings.HasSuffix(a, "d"):
			return 4
		case strings.HasSuffix(a, "h"):
			return 3
		case strings.HasSuffix(a, "m"):
			return 2
		case strings.HasSuffix(a, "s"):
			return 1
		}
		return 0
	}
	first, last := ages[0], ages[len(ages)-1]
	if rank(last) > rank(first) {
		t.Errorf("expected newest-last sort, but first=%q is newer than last=%q\noutput:\n%s", first, last, out)
	}
}

// Unknown kind exits non-zero with a helpful error.
func TestSort_UnknownKind(t *testing.T) {
	_, errOut, code := run("sort", "bogus")
	if code == 0 {
		t.Error("expected non-zero exit for unknown sort kind")
	}
	if !strings.Contains(errOut, "unknown sort kind") {
		t.Errorf("expected unknown-kind error in stderr:\n%s", errOut)
	}
}

// Missing kind exits non-zero.
func TestSort_MissingKind(t *testing.T) {
	_, errOut, code := run("sort")
	if code == 0 {
		t.Error("expected non-zero exit when kind is missing")
	}
	if !strings.Contains(errOut, "sort requires a kind") {
		t.Errorf("expected missing-kind error in stderr:\n%s", errOut)
	}
}

// -h prints usage to stdout.
func TestSort_Help(t *testing.T) {
	out, _, code := run("sort", "-h")
	if code != 0 {
		t.Fatalf("expected exit 0 for -h, got %d", code)
	}
	for _, want := range []string{"--namespace", "--all-namespaces", "Examples:", "Kinds:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in sort help output:\n%s", want, out)
		}
	}
}

// ── global ────────────────────────────────────────────────────────────────────

// Unknown top-level command exits non-zero.
func TestUnknownCommand(t *testing.T) {
	_, errOut, code := run("bogus")
	if code == 0 {
		t.Error("expected non-zero exit for unknown command")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// --help exits 0 and prints usage.
func TestHelp(t *testing.T) {
	out, _, code := run("--help")
	if code != 0 {
		t.Fatalf("expected exit 0 for --help, got %d", code)
	}
	if !strings.Contains(out, "kdiag") {
		t.Errorf("expected kdiag in help output:\n%s", out)
	}
}

// Nested help: each level of the tree responds to -h with its own scope.
// Root help must stay compact (no per-kind descriptions); subcommand help
// must list its children.
func TestNestedHelp(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		wantCode  int
		contains  []string
		excludes  []string
	}{
		{
			name:     "root --help",
			args:     []string{"--help"},
			wantCode: 0,
			// The branded title line belongs to the help screen only.
			contains: []string{"kdiag — Kubernetes diagnostic CLI", "inspect", "diff", "completion", "events", "Usage:"},
			// Per-kind descriptions belong one level down. `version` is a flag
			// (`--version`), not a subcommand, so it must not appear in help.
			excludes: []string{
				"Show container state for all pods in a deployment",
				"Show container state for all pods in a daemonset",
				// Old "rs diff" wording must not survive the rename.
				"rs diff",
				// az pods is now under inspect --az.
				"az pods",
				// `version` subcommand has been removed.
				"version",
			},
		},
		{
			// Regression guard: error-fallback usage (unknown command) must
			// not print the branded title — only the help screen does. The
			// usage block (available commands + hint) still appears.
			name:     "unknown command",
			args:     []string{"definitely-not-a-command"},
			wantCode: 1,
			contains: []string{"unknown command", "Available Commands:", "Usage:"},
			excludes: []string{"kdiag — Kubernetes diagnostic CLI"},
		},
		{
			// Bare `kdiag` prints only the banner + pointer — no command
			// list. The command list belongs to `kdiag -h` / `kdiag help`.
			name:     "no args",
			args:     []string{},
			wantCode: 1,
			contains: []string{"kdiag — Kubernetes diagnostic CLI", "Usage:", `kdiag -h`},
			excludes: []string{"Available Commands:", "inspect ", "diff "},
		},
		{
			name:     "inspect -h",
			args:     []string{"inspect", "-h"},
			wantCode: 0,
			contains: []string{"pod", "deploy", "ds", "sts", "rs", "node", "Examples:"},
		},
		{
			name:     "inspect (no args)",
			args:     []string{"inspect"},
			wantCode: 1,
			// Stderr asserted via exit code; full content covered above.
		},
		{
			name:     "az (removed)",
			args:     []string{"az", "--help"},
			wantCode: 1,
			// az is no longer a top-level command; functionality is under inspect --az.
		},
		{
			name:     "diff --help",
			args:     []string{"diff", "--help"},
			wantCode: 0,
			contains: []string{"rs", "pod", "node", "Examples:"},
		},
		{
			name:     "diff pod --help",
			args:     []string{"diff", "pod", "--help"},
			wantCode: 0,
			contains: []string{"--namespace", "Examples:"},
		},
		{
			name:     "diff node --help",
			args:     []string{"diff", "node", "--help"},
			wantCode: 0,
			contains: []string{"Examples:"},
		},
		{
			name:     "completion --help",
			args:     []string{"completion", "--help"},
			wantCode: 0,
			contains: []string{"bash", "zsh"},
		},
		{
			name:     "inspect pod --help",
			args:     []string{"inspect", "pod", "--help"},
			wantCode: 0,
			contains: []string{"--label", "--namespace", "--resources", "--output", "Examples:"},
		},
		{
			name:     "inspect deploy -h",
			args:     []string{"inspect", "deploy", "-h"},
			wantCode: 0,
			// Format flag must be advertised in deploy help.
			contains: []string{"--namespace", "--resources", "--deployment-spec", "--output", "Examples:"},
		},
		{
			name:     "inspect node -h",
			args:     []string{"inspect", "node", "-h"},
			wantCode: 0,
			contains: []string{"--label", "Examples:"},
		},
		{
			name:     "diff rs --help",
			args:     []string{"diff", "rs", "--help"},
			wantCode: 0,
			contains: []string{"--label", "Examples:"},
		},
		{
			name:     "events -h",
			args:     []string{"events", "-h"},
			wantCode: 0,
			contains: []string{"--namespace", "--all-namespaces", "--since", "Examples:"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := run(tc.args...)
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", code, tc.wantCode, out, errOut)
			}
			// Help (exit 0) goes to stdout; error (non-zero) goes to stderr.
			text := out
			if tc.wantCode != 0 {
				text = errOut
			}
			for _, want := range tc.contains {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q in output:\n%s", want, text)
				}
			}
			for _, banned := range tc.excludes {
				if strings.Contains(text, banned) {
					t.Errorf("unexpected %q in output:\n%s", banned, text)
				}
			}
		})
	}
}

// `kdiag --version` prints the stamped build metadata, in the format
// `<version> (built <buildTime>, commit <commit>)` per the global Go rule
// (no app-name prefix). buildTime must not be the default `unknown` —
// TestMain stamps it via -ldflags. The `version` subcommand was removed;
// only the flag is supported now.
func TestVersion(t *testing.T) {
	out, _, code := run("--version")
	if code != 0 {
		t.Fatalf("expected exit 0 for --version, got %d", code)
	}
	// Output must not contain the app-name prefix anywhere.
	if strings.Contains(out, "kdiag") {
		t.Errorf("version output should not contain 'kdiag' prefix:\n%s", out)
	}
	// Stamped version is "integration"; format also requires "(built " and "commit ".
	for _, want := range []string{"integration", "(built ", "commit "} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in --version output:\n%s", want, out)
		}
	}
	if strings.Contains(out, "unknown") {
		t.Errorf("expected stamped buildTime (got default 'unknown'):\n%s", out)
	}
}

// `kdiag version` (subcommand) is no longer accepted — it must surface as an
// unknown-command error like any other unrecognised arg.
func TestVersionSubcommandRemoved(t *testing.T) {
	out, errOut, code := run("version")
	if code == 0 {
		t.Fatalf("expected non-zero exit for `version` subcommand, got 0\nstdout:\n%s\nstderr:\n%s", out, errOut)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Errorf("expected 'unknown command' in stderr:\n%s", errOut)
	}
}

// ── completion ────────────────────────────────────────────────────────────────

// `kdiag completion <shell>` emits a non-empty script for each supported shell.
func TestCompletion_Shells(t *testing.T) {
	cases := []struct {
		shell  string
		marker string // a string we expect to find in that shell's script
	}{
		{"bash", "complete -F _kdiag kdiag"},
		{"zsh", "#compdef kdiag"},
	}
	for _, tc := range cases {
		out, _, code := run("completion", tc.shell)
		if code != 0 {
			t.Errorf("expected exit 0 for completion %s, got %d", tc.shell, code)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("expected non-empty output for completion %s", tc.shell)
		}
		if !strings.Contains(out, tc.marker) {
			t.Errorf("expected %q in completion %s output:\n%s", tc.marker, tc.shell, out)
		}
	}
}

// Unknown shell exits non-zero with a clear error.
func TestCompletion_UnknownShell(t *testing.T) {
	_, errOut, code := run("completion", "powershell")
	if code == 0 {
		t.Error("expected non-zero exit for unknown shell")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// ── __complete (dynamic completion helper) ──────────────────────────────────

// `kdiag __complete namespaces` lists every namespace. Filtering by prefix
// narrows it to the kdiag-test fixture.
func TestComplete_Namespaces(t *testing.T) {
	all, _, code := run("__complete", "namespaces")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"default", "kdiag-test", "kube-system"} {
		if !strings.Contains(all, want) {
			t.Errorf("expected %q in namespaces output:\n%s", want, all)
		}
	}

	// Prefix filter.
	out, _, code := run("__complete", "namespaces", "kdiag")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if strings.TrimSpace(out) != "kdiag-test" {
		t.Errorf("expected only kdiag-test for prefix 'kdiag', got:\n%s", out)
	}
	if strings.Contains(out, "kube-system") {
		t.Errorf("prefix filter should exclude kube-system:\n%s", out)
	}
}

// `kdiag __complete resources <kind> <ns>` lists names in the namespace.
// Covers the kinds the user can autocomplete: pod, deploy, ds, sts, rs,
// node (cluster-scoped). Empty ns falls back to current-context.
func TestComplete_Resources(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		ns      string
		prefix  string
		want    string // a name that must appear in the output
		exclude string // a string that must NOT appear (sanity check)
	}{
		{"deploy in kdiag-test", "deploy", "kdiag-test", "", "test-app", ""},
		{"deployment alias", "deployment", "kdiag-test", "", "test-app", ""},
		{"pod prefix kdiag", "pod", "kdiag-test", "kdiag", "kdiag-static", "test-app"},
		{"ds in kdiag-test", "ds", "kdiag-test", "", "kdiag-ds", ""},
		{"sts in kdiag-test", "sts", "kdiag-test", "", "kdiag-sts", ""},
		{"node (cluster-scoped)", "node", "", "", "kdiag-test-control-plane", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"__complete", "resources", tc.kind, tc.ns}
			if tc.prefix != "" {
				args = append(args, tc.prefix)
			}
			out, _, code := run(args...)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d\nargs: %v\nout: %s", code, args, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected %q in output:\n%s", tc.want, out)
			}
			if tc.exclude != "" && strings.Contains(out, tc.exclude) {
				t.Errorf("prefix filter should have excluded %q:\n%s", tc.exclude, out)
			}
		})
	}
}

// Unknown kinds and missing args are silent (exit 0, no output) so the
// shell doesn't spew errors at the user's cursor.
func TestComplete_SilentOnBadInput(t *testing.T) {
	cases := [][]string{
		{"__complete"},                              // no subcommand
		{"__complete", "resources"},                 // missing kind
		{"__complete", "resources", "thingamajig"},  // unknown kind
		{"__complete", "bogus"},                     // unknown subcommand
	}
	for _, args := range cases {
		out, errOut, code := run(args...)
		if code != 0 {
			t.Errorf("expected exit 0 for %v, got %d (stderr: %s)", args, code, errOut)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("expected empty stdout for %v, got:\n%s", args, out)
		}
	}
}

// Missing shell argument exits non-zero.
func TestCompletion_MissingShell(t *testing.T) {
	_, errOut, code := run("completion")
	if code == 0 {
		t.Error("expected non-zero exit when shell argument is missing")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// ── help filtering for view-aware modes ───────────────────────────────────────

// View-aware help: passing --path to -h hides --output/--resources/--az.
func TestInspectPod_HelpFiltered_Path(t *testing.T) {
	out, _, code := run("inspect", "pod", "--path", "memory", "-h")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "--path") {
		t.Errorf("help missing --path:\n%s", out)
	}
	for _, flag := range []string{"--output", "--resources", "--az"} {
		if strings.Contains(out, flag) {
			t.Errorf("help unexpectedly contains %q:\n%s", flag, out)
		}
	}
}

func TestInspectPod_HelpUnfiltered(t *testing.T) {
	out, _, code := run("inspect", "pod", "-h")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	for _, flag := range []string{"--path", "--output", "--resources", "--az"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help missing %q:\n%s", flag, out)
		}
	}
}

// ── inspect --path ──────────────────────────────────────────────────────

func TestInspect_YMLPath_DeploymentMemory(t *testing.T) {
	out, _, code := run("inspect", "deploy", "kdiag-multicont", "-n", "kdiag-test", "--path", "memory")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	for _, want := range []string{
		"api:",
		"sidecar:",
		".spec.template.spec.containers[0].resources.limits.memory",
		".spec.template.spec.containers[0].resources.requests.memory",
		".spec.template.spec.containers[1].resources.limits.memory",
		".spec.template.spec.containers[1].resources.requests.memory",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Deployment/kdiag-multicont:") {
		t.Errorf("name-mode output should not carry the outer Kind/name header:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		// Path lines start with "." (yq path) and must not end with ": <value>".
		if strings.HasPrefix(trimmed, ".spec.") && strings.Contains(trimmed, ": ") {
			t.Errorf("path line carries a value suffix: %q", trimmed)
		}
	}
}

func TestInspect_YMLPath_PipeIntoYQ(t *testing.T) {
	if _, err := exec.LookPath("yq"); err != nil {
		t.Skip("yq not installed in PATH; skipping pipeline test")
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		t.Skip("kubectl not installed in PATH; skipping pipeline test")
	}
	out, _, code := run("inspect", "deploy", "kdiag-multicont", "-n", "kdiag-test", "--path", "memory")
	if code != 0 {
		t.Fatalf("kdiag exit %d:\n%s", code, out)
	}
	var firstPath string
	for _, line := range strings.Split(out, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, ".spec.") {
			firstPath = s
			break
		}
	}
	if firstPath == "" {
		t.Fatalf("no path emitted:\n%s", out)
	}
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf(`kubectl get deploy kdiag-multicont -n kdiag-test -o yaml | yq '%s'`, firstPath))
	cmd.Env = append(os.Environ(), "KUBECONFIG="+os.Getenv("KUBECONFIG"))
	yqOut, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("yq pipeline failed: %v\n%s", err, yqOut)
	}
	trimmed := strings.TrimSpace(string(yqOut))
	if trimmed == "" || trimmed == "null" {
		t.Errorf("yq %q returned empty/null:\n%s", firstPath, yqOut)
	}
}

func TestInspect_YMLPath_Selector(t *testing.T) {
	out, _, code := run("inspect", "deploy", "-n", "kdiag-test", "-l", "app=kdiag-multicont", "--path", "memory")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Deployment/kdiag-multicont:") {
		t.Errorf("missing outer Kind/name header in selector mode:\n%s", out)
	}
	if !strings.Contains(out, "  api:") || !strings.Contains(out, "  sidecar:") {
		t.Errorf("missing indented per-container headers:\n%s", out)
	}
}

func TestInspect_YMLPath_ConflictWithYAML(t *testing.T) {
	out, errOut, code := run("inspect", "deploy", "kdiag-multicont", "-n", "kdiag-test", "--path", "memory", "-o", "yaml")
	if code == 0 {
		t.Fatalf("expected non-zero exit:\n%s", out)
	}
	combined := out + errOut
	if !strings.Contains(combined, "--path") || (!strings.Contains(combined, "--output") && !strings.Contains(combined, "-o")) {
		t.Errorf("conflict error should name both flags:\n%s", combined)
	}
}

func TestInspect_YMLPath_FindPathRemoved(t *testing.T) {
	_, errOut, code := run("inspect", "deploy", "kdiag-multicont", "-n", "kdiag-test", "--find-path", "memory")
	if code == 0 {
		t.Fatal("--find-path should be rejected as unknown")
	}
	if !strings.Contains(errOut, "find-path") && !strings.Contains(errOut, "unknown flag") {
		t.Errorf("expected unknown-flag error, got: %s", errOut)
	}
}

func TestInspect_YMLPath_ClusterScopedNode(t *testing.T) {
	// kind clusters set kubernetes.io/hostname on every node; case-sensitive needle.
	out, _, code := run("inspect", "node", "-l", "kubernetes.io/hostname", "--path", "*hostname*")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	// We don't assert the exact label keys (kind versions vary), only that
	// the command produced *some* match output and a Node/<name>: header.
	if !strings.Contains(out, "Node/") {
		t.Errorf("expected `Node/<name>:` header for cluster-scoped node output:\n%s", out)
	}
}

func TestInspect_YMLPath_NoMatchExitsZero(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--path", "ZZZ-no-such-string-ZZZ")
	if code != 0 {
		t.Fatalf("expected exit 0 with no matches, got %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty stdout, got:\n%s", out)
	}
}

func TestInspect_YMLPath_MissingTarget(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "-n", "kdiag-test", "--path", "qos")
	if code == 0 {
		t.Error("expected non-zero exit when neither name nor selector is given")
	}
	if !strings.Contains(errOut, "--path") {
		t.Errorf("expected `--path` in error stderr:\n%s", errOut)
	}
}

func TestInspect_YMLPath_EmptyValueErrors(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--path=")
	if code == 0 {
		t.Error("expected non-zero exit for empty --path value")
	}
	if !strings.Contains(errOut, "non-empty") {
		t.Errorf("expected `non-empty` in error stderr:\n%s", errOut)
	}
}

func TestInspect_YMLPath_WhitespaceNeedleErrors(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--path", "   ")
	if code == 0 {
		t.Error("expected non-zero exit for whitespace-only --path value")
	}
	if !strings.Contains(errOut, "non-empty") {
		t.Errorf("expected `non-empty` in error stderr:\n%s", errOut)
	}
}

func TestInspect_YMLPath_CRD(t *testing.T) {
	// CRD coverage: dynamic client resolves the kind and walks unstructured.
	// Output dropped the ": <value>" suffix in the redesign, so we only
	// assert the path is present.
	out, _, code := run("inspect", "widgets.kdiag.test", "kdiag-widget", "-n", "kdiag-test", "--path", "renewBefore")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, ".spec.renewBefore") {
		t.Errorf("expected `.spec.renewBefore` in CRD output:\n%s", out)
	}
}

// ── help dispatch & banner ────────────────────────────────────────────────────

// `kdiag` (no args) must print only the banner + pointer — no command list.
// Errors stay terse; the command list is reserved for `kdiag -h`.
func TestRoot_BareInvocationHidesCommandList(t *testing.T) {
	out, errOut, code := run()
	if code == 0 {
		t.Error("expected non-zero exit for bare `kdiag`")
	}
	combined := out + errOut
	if !strings.Contains(combined, "kdiag — Kubernetes diagnostic CLI") {
		t.Errorf("expected banner line in output:\n%s", combined)
	}
	if !strings.Contains(combined, `kdiag -h`) {
		t.Errorf("expected hint to `kdiag -h`:\n%s", combined)
	}
	for _, banned := range []string{"Available Commands", "inspect ", "diff ", "events ", "sort "} {
		if strings.Contains(combined, banned) {
			t.Errorf("bare invocation should not enumerate %q:\n%s", banned, combined)
		}
	}
}

// `kdiag -h` keeps the command list (regression guard for the change above).
func TestRoot_DashHListsCommands(t *testing.T) {
	out, _, code := run("-h")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d:\n%s", code, out)
	}
	for _, want := range []string{"inspect", "diff", "events", "sort", "Available Commands"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in `kdiag -h` output:\n%s", want, out)
		}
	}
}

// `kdiag help <cmd>` is a git-style alias for `kdiag <cmd> -h`. Spot-check
// each subcommand so a future dispatch refactor can't silently drop one.
func TestHelp_DispatchesPerCommand(t *testing.T) {
	cases := []struct {
		topic string
		want  string
	}{
		{"inspect", "Available Subcommands"},
		{"diff", "Diff two Kubernetes resources"},
		{"events", "Show events"},
		{"sort", "Sort resources by creation date"},
		{"completion", "Generate a shell completion script"},
	}
	for _, c := range cases {
		t.Run(c.topic, func(t *testing.T) {
			out, _, code := run("help", c.topic)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d:\n%s", code, out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("expected %q in `kdiag help %s` output:\n%s", c.want, c.topic, out)
			}
		})
	}
}

// `kdiag help inspect pod` must route to the same printer as `inspect pod -h`.
func TestHelp_NestedSubcommand(t *testing.T) {
	viaHelp, _, code := run("help", "inspect", "pod")
	if code != 0 {
		t.Fatalf("help: expected exit 0, got %d:\n%s", code, viaHelp)
	}
	viaDash, _, code := run("inspect", "pod", "-h")
	if code != 0 {
		t.Fatalf("dash: expected exit 0, got %d:\n%s", code, viaDash)
	}
	if viaHelp != viaDash {
		t.Errorf("`kdiag help inspect pod` differs from `kdiag inspect pod -h`:\n--help--\n%s\n--dash--\n%s",
			viaHelp, viaDash)
	}
}

// `kdiag help yml-path` prints the topic page (legacy name preserved even
// though the flag is `--path`). `kdiag help path` is accepted too.
func TestHelp_YMLPathTopic(t *testing.T) {
	for _, alias := range []string{"yml-path", "path"} {
		t.Run(alias, func(t *testing.T) {
			out, _, code := run("help", alias)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d:\n%s", code, out)
			}
			for _, want := range []string{
				"--path <needle>",
				"Walk the resource YAML",
				"Smart-case",
				"Examples:",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in topic output:\n%s", want, out)
				}
			}
		})
	}
}

func TestHelp_UnknownTopicErrors(t *testing.T) {
	_, errOut, code := run("help", "bogus-topic")
	if code == 0 {
		t.Error("expected non-zero exit for unknown help topic")
	}
	if !strings.Contains(errOut, "unknown help topic") {
		t.Errorf("expected `unknown help topic` in stderr:\n%s", errOut)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func assertContainerInfo(t *testing.T, out string) {
	t.Helper()
	for _, want := range []string{"Container:", "State:", "Ready:", "Restart Count:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}
