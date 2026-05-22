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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	// Stamp ldflags so the `version` command reports meaningful values.
	buildDate := time.Now().UTC().Format("02-01-06_15:04")
	ldflags := fmt.Sprintf(
		"-X main.Version=integration -X main.BuildDate=%s -X main.Commit=test",
		buildDate,
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
	if !strings.Contains(out, "Namespace: kdiag-test") {
		t.Errorf("expected namespace header in output:\n%s", out)
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

// `inspect pod --resources` emits a YAML list of {name, resources} per
// container. With a positional pod name, the output is a flat sequence — no
// banner lines — so it pipes straight into yq.
func TestInspectPod_Resources(t *testing.T) {
	out, _, code := run("inspect", "pod", "--resources", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.HasPrefix(strings.TrimLeft(out, "\n"), "- ") {
		t.Errorf("expected YAML sequence (starts with '- '):\n%s", out)
	}
	for _, want := range []string{"name:", "resources:", "requests:", "limits:", "cpu", "memory"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in YAML output:\n%s", want, out)
		}
	}
	for _, banned := range []string{"Pod:", "Container:", "Namespace:"} {
		if strings.Contains(out, banned) {
			t.Errorf("YAML mode should not include text header %q:\n%s", banned, out)
		}
	}
}

// `inspect pod --container-spec` emits .spec.containers[] as YAML when given
// a positional pod name.
func TestInspectPod_ContainerSpec_YAML(t *testing.T) {
	out, _, code := run("inspect", "pod", "--container-spec", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.HasPrefix(strings.TrimLeft(out, "\n"), "- ") {
		t.Errorf("expected YAML sequence (starts with '- '):\n%s", out)
	}
	for _, want := range []string{"name:", "image:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in YAML output:\n%s", want, out)
		}
	}
}

// `inspect pod --resources -l <label>` matching multiple pods emits a YAML map
// keyed by pod name. The shape is chosen by input (--label vs positional), so
// the map form is used even for a single match.
func TestInspectPod_Resources_LabelMap(t *testing.T) {
	out, _, code := run("inspect", "pod", "--resources", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if strings.HasPrefix(strings.TrimLeft(out, "\n"), "- ") {
		t.Errorf("expected YAML map (not sequence) when --label is used:\n%s", out)
	}
	for _, want := range []string{"resources:", "name:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in YAML output:\n%s", want, out)
		}
	}
}

// `inspect pod --container-spec` + `--resources` mutually exclusive.
func TestInspectPod_YAMLFlags_MutuallyExclusive(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "--container-spec", "--resources", "-n", "kdiag-test", "kdiag-static")
	if code == 0 {
		t.Error("expected non-zero exit when both YAML flags are combined")
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' error in stderr:\n%s", errOut)
	}
}

// YAML flag combined with --az must error on pod too.
func TestInspectPod_YAMLFlags_NotWithAZ(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "--az", "--resources", "-n", "kdiag-test", "kdiag-static")
	if code == 0 {
		t.Error("expected non-zero exit when YAML flag combined with --az")
	}
	if !strings.Contains(errOut, "--az cannot be combined") {
		t.Errorf("expected '--az cannot be combined' error in stderr:\n%s", errOut)
	}
}

// The removed --kubeconfig / --context flags must be reported unknown.
func TestInspect_RemovedFlags_Rejected(t *testing.T) {
	cases := [][]string{
		{"inspect", "pod", "--kubeconfig", "/tmp/x", "-n", "kdiag-test"},
		{"inspect", "pod", "--context", "ignored", "-n", "kdiag-test"},
		{"inspect", "pod", "--selector", "app=test-app", "-n", "kdiag-test"},
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

// ── inspect --yaml-field ─────────────────────────────────────────────────────

// Search for a value (case-sensitive: needle has uppercase).
// `kdiag-static` runs at QoS class Burstable thanks to its requests+limits.
func TestInspectYAMLField_PodValue(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--yaml-field", "Burstable")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, ".status.qosClass: Burstable") {
		t.Errorf("expected `.status.qosClass: Burstable` in output:\n%s", out)
	}
}

// Search for a key with smart-case (lowercase needle → case-insensitive).
// Deployment containers must produce generalized `[]` array paths. The
// `test-app` fixture has a single container, so the `# name=` annotation
// is suppressed (nothing to disambiguate). Multi-container annotation is
// covered by unit tests.
func TestInspectYAMLField_DeployKey_SmartCase(t *testing.T) {
	out, _, code := run("inspect", "deploy", "test-app", "-n", "kdiag-test", "--yaml-field", "imagepull")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, ".spec.template.spec.containers[].imagePullPolicy") {
		t.Errorf("expected container imagePullPolicy path in output:\n%s", out)
	}
	if strings.Contains(out, "# name=") {
		t.Errorf("did not expect `# name=` annotation for single-container deployment:\n%s", out)
	}
}

// Label-selector mode prints one block per matched resource, prefixed with the kind/name header.
func TestInspectYAMLField_LabelSelector(t *testing.T) {
	out, _, code := run("inspect", "pod", "-l", "app=test-app", "-n", "kdiag-test", "--yaml-field", "qosClass")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	// At least one pod/<name>: header and the qosClass match line indented.
	if !strings.Contains(out, "Pod/") {
		t.Errorf("expected `Pod/<name>:` header in selector-mode output:\n%s", out)
	}
	if !strings.Contains(out, ".status.qosClass:") {
		t.Errorf("expected `.status.qosClass:` line in output:\n%s", out)
	}
}

// Cluster-scoped kind: nodes are namespace-less; --yaml-field must still work.
func TestInspectYAMLField_ClusterScopedNode(t *testing.T) {
	// kind clusters set kubernetes.io/hostname on every node; case-sensitive needle.
	out, _, code := run("inspect", "node", "-l", "kubernetes.io/hostname", "--yaml-field", "hostname")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	// We don't assert the exact label keys (kind versions vary), only that
	// the command produced *some* match output and a Node/<name>: header.
	if !strings.Contains(out, "Node/") {
		t.Errorf("expected `Node/<name>:` header for cluster-scoped node output:\n%s", out)
	}
}

// No matches → exit 0 with empty stdout (grep semantics).
func TestInspectYAMLField_NoMatchExitsZero(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--yaml-field", "ZZZ-no-such-string-ZZZ")
	if code != 0 {
		t.Fatalf("expected exit 0 with no matches, got %d", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty stdout, got:\n%s", out)
	}
}

// Providing neither <name> nor -l errors.
func TestInspectYAMLField_MissingTarget(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "-n", "kdiag-test", "--yaml-field", "qos")
	if code == 0 {
		t.Error("expected non-zero exit when neither name nor selector is given")
	}
	if !strings.Contains(errOut, "--yaml-field") {
		t.Errorf("expected `--yaml-field` in error stderr:\n%s", errOut)
	}
}

// `--yaml-field=` (empty value) must error explicitly rather than silently
// falling through to the per-kind handler and producing a confusing
// "unknown flag" message.
func TestInspectYAMLField_EmptyValueErrors(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--yaml-field=")
	if code == 0 {
		t.Error("expected non-zero exit for empty --yaml-field value")
	}
	if !strings.Contains(errOut, "non-empty") {
		t.Errorf("expected `non-empty` in error stderr:\n%s", errOut)
	}
}

// Whitespace-only needle is rejected too — it would otherwise match any
// scalar containing the same whitespace.
func TestInspectYAMLField_WhitespaceNeedleErrors(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--yaml-field", "   ")
	if code == 0 {
		t.Error("expected non-zero exit for whitespace-only --yaml-field value")
	}
	if !strings.Contains(errOut, "non-empty") {
		t.Errorf("expected `non-empty` in error stderr:\n%s", errOut)
	}
}

// Unknown flags alongside --yaml-field error with a clear message instead
// of being silently dropped.
func TestInspectYAMLField_UnknownFlagErrors(t *testing.T) {
	_, errOut, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test", "--yaml-field", "qos", "--bogus")
	if code == 0 {
		t.Error("expected non-zero exit for unknown flag alongside --yaml-field")
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Errorf("expected `unknown flag` in error stderr:\n%s", errOut)
	}
}

// Multi-line scalar values must render Go-quoted so the path:value line
// stays single-line and yq-pipeable.
func TestInspectYAMLField_MultilineConfigMapValue(t *testing.T) {
	out, _, code := run("inspect", "cm", "kdiag-cm-multiline", "-n", "kdiag-test", "--yaml-field", "needle-line-two")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	// The `.data.config:` match must render as one Go-quoted line, with
	// literal `\n` escapes rather than real newlines that would orphan
	// the trailing content. (`needle-line-two` also appears inside
	// kubectl's `last-applied-configuration` annotation — both matches
	// are valid, both must be on single lines.)
	want := `.data.config: "line-one\nneedle-line-two\nline-three\n"`
	if !strings.Contains(out, want) {
		t.Errorf("expected single-line Go-quoted match %q in output:\n%s", want, out)
	}
}

// CRD support — the dynamic client must walk a user-defined kind the same
// way it walks built-ins. The Widget fixture exercises this.
func TestInspectYAMLField_CRD(t *testing.T) {
	out, _, code := run("inspect", "widgets.kdiag.test", "kdiag-widget", "-n", "kdiag-test", "--yaml-field", "renewBefore")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, ".spec.renewBefore: 24h") {
		t.Errorf("expected `.spec.renewBefore: 24h` in CRD output:\n%s", out)
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

// `inspect deploy --resources` emits the deployment template's per-container
// resources as YAML (issue #41). Should NOT iterate pods or show "Pod:" /
// "Container:" headers from the text mode.
func TestInspectDeploy_Resources_YAML(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--resources", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"- name: nginx", "resources:", "requests:", "limits:", "cpu: 50m", "memory: 32Mi"} {
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

// `inspect deploy --spec` emits .spec.template.spec as YAML. Keys are
// alphabetized by sigs.k8s.io/yaml (JSON marshalling), so "image" precedes
// "name" inside each container entry — assert the keys separately, not as
// a sequence-marker line.
func TestInspectDeploy_Spec_YAML(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--spec", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"containers:", "name: nginx", "image: nginx:alpine"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in YAML output:\n%s", want, out)
		}
	}
}

// `inspect deploy --container-spec` emits .spec.template.spec.containers[] as YAML.
func TestInspectDeploy_ContainerSpec_YAML(t *testing.T) {
	out, _, code := run("inspect", "deploy", "--container-spec", "-n", "kdiag-test", "test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	// Output must begin with a YAML sequence marker (one entry per container).
	if !strings.HasPrefix(strings.TrimLeft(out, "\n"), "- ") {
		t.Errorf("expected YAML sequence (starts with '- '):\n%s", out)
	}
	for _, want := range []string{"name: nginx", "image: nginx:alpine", "resources:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in YAML output:\n%s", want, out)
		}
	}
}

// Mutually exclusive YAML flags must error.
func TestInspectDeploy_YAMLFlags_MutuallyExclusive(t *testing.T) {
	_, errOut, code := run("inspect", "deploy", "--spec", "--container-spec", "-n", "kdiag-test", "test-app")
	if code == 0 {
		t.Error("expected non-zero exit when two YAML flags are combined")
	}
	if !strings.Contains(errOut, "mutually exclusive") {
		t.Errorf("expected 'mutually exclusive' error in stderr:\n%s", errOut)
	}
}

// YAML flag combined with --az must error.
func TestInspectDeploy_YAMLFlags_NotWithAZ(t *testing.T) {
	_, errOut, code := run("inspect", "deploy", "--az", "--spec", "-n", "kdiag-test", "test-app")
	if code == 0 {
		t.Error("expected non-zero exit when YAML flag is combined with --az")
	}
	if !strings.Contains(errOut, "--az cannot be combined") {
		t.Errorf("expected '--az cannot be combined' error in stderr:\n%s", errOut)
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
	for _, want := range []string{"Namespace: kdiag-test", "POD", "NODE", "ZONE", "Summary"} {
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
	for _, want := range []string{"Namespace: kdiag-test", "Deployment: test-app", "POD", "NODE", "ZONE", "Summary"} {
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
		"Namespace: kdiag-test",
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
	if !strings.Contains(out, "Namespace: kdiag-test") {
		t.Errorf("expected namespace header in output:\n%s", out)
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

// Pod listing: header is present, namespace banner echoes -n, the static pod
// fixture appears. Smoke test that the command runs and yields a table.
func TestSort_Pods(t *testing.T) {
	out, _, code := run("sort", "pod", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	for _, want := range []string{"Namespace: kdiag-test", "Kind: pod", "AGE", "CREATED", "NAME", "kdiag-static"} {
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
			contains: []string{"inspect", "diff", "completion", "events", "version", "Usage:"},
			// Per-kind descriptions belong one level down.
			excludes: []string{
				"Show container state for all pods in a deployment",
				"Show container state for all pods in a daemonset",
				// Old "rs diff" wording must not survive the rename.
				"rs diff",
				// az pods is now under inspect --az.
				"az pods",
			},
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
			contains: []string{"--label", "--namespace", "--resources", "--container-spec", "Examples:"},
		},
		{
			name:     "inspect deploy -h",
			args:     []string{"inspect", "deploy", "-h"},
			wantCode: 0,
			// YAML-mode flags must be advertised in deploy help.
			contains: []string{"--namespace", "--resources", "--spec", "--container-spec", "Examples:"},
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

// `kdiag version` and `kdiag --version` print the stamped build metadata.
// BuildDate must not be the default `unknown` — TestMain stamps it via -ldflags.
func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		out, _, code := run(arg)
		if code != 0 {
			t.Fatalf("expected exit 0 for %q, got %d", arg, code)
		}
		if !strings.Contains(out, "kdiag") {
			t.Errorf("expected %q in output for %q:\n%s", "kdiag", arg, out)
		}
		if strings.Contains(out, "unknown") {
			t.Errorf("expected stamped BuildDate (got default 'unknown') for %q:\n%s", arg, out)
		}
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

// ── helpers ───────────────────────────────────────────────────────────────────

func assertContainerInfo(t *testing.T, out string) {
	t.Helper()
	for _, want := range []string{"Container:", "State:", "Ready:", "Restart Count:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}
