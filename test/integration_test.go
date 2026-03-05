//go:build integration

// Integration tests require a running Kubernetes cluster and the KUBECONFIG
// environment variable pointing to it.
//
// Quick start with kind:
//
//	make cluster-up
//	make test-integration
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
	build := exec.Command("go", "build", "-o", binaryPath, ".")
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

// Single pod by name — flag before pod name.
func TestInspectPod_ByName_FlagFirst(t *testing.T) {
	out, _, code := run("inspect", "pod", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	assertContainerInfo(t, out)
}

// Single pod by name — pod name before flags (kubectl-like ordering).
func TestInspectPod_ByName_PodNameFirst(t *testing.T) {
	out, _, code := run("inspect", "pod", "kdiag-static", "-n", "kdiag-test")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	assertContainerInfo(t, out)
}

// --resources flag shows CPU/memory requests and limits.
func TestInspectPod_Resources(t *testing.T) {
	out, _, code := run("inspect", "pod", "--resources", "-n", "kdiag-test", "kdiag-static")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"Requests:", "Limits:", "cpu", "memory"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
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

// ── az pods ───────────────────────────────────────────────────────────────────

// Normal AZ pods run shows the placement table and summary.
func TestAZPods_BySelector(t *testing.T) {
	out, _, code := run("az", "pods", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{"Namespace: kdiag-test", "POD", "NODE", "ZONE", "Summary"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}

// Missing selector exits non-zero with a clear error.
func TestAZPods_NoSelector_Error(t *testing.T) {
	_, errOut, code := run("az", "pods", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when --selector is missing")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// Selector with no matches prints a clear message.
func TestAZPods_NoMatch(t *testing.T) {
	out, _, code := run("az", "pods", "-n", "kdiag-test", "-l", "app=does-not-exist")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "No pods found") {
		t.Errorf("expected 'No pods found' in output:\n%s", out)
	}
}

// Missing subcommand (just `az`) exits non-zero.
func TestAZ_MissingSubcommand(t *testing.T) {
	_, errOut, code := run("az")
	if code == 0 {
		t.Error("expected non-zero exit for missing subcommand")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

// ── rs diff ───────────────────────────────────────────────────────────────────

func TestRSDiff_ByName(t *testing.T) {
	out, _, code := run("rs", "diff", "-n", "kdiag-test", "test-app")
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
	out, _, code := run("rs", "diff", "-n", "kdiag-test", "-l", "app=test-app")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, out)
	}
	if !strings.Contains(out, "Deployment: kdiag-test/test-app") {
		t.Errorf("expected deployment header in output:\n%s", out)
	}
}

func TestRSDiff_MissingSubcommand(t *testing.T) {
	_, errOut, code := run("rs")
	if code == 0 {
		t.Error("expected non-zero exit for missing subcommand")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

func TestRSDiff_NoArgsNoSelector(t *testing.T) {
	_, errOut, code := run("rs", "diff", "-n", "kdiag-test")
	if code == 0 {
		t.Error("expected non-zero exit when neither name nor selector given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

func TestRSDiff_NameAndSelector_Error(t *testing.T) {
	_, errOut, code := run("rs", "diff", "-n", "kdiag-test", "test-app", "-l", "app=test-app")
	if code == 0 {
		t.Error("expected non-zero exit when both name and selector given")
	}
	if !strings.Contains(errOut, "Error:") {
		t.Errorf("expected error message in stderr:\n%s", errOut)
	}
}

func TestRSDiff_NoMatch_Error(t *testing.T) {
	_, errOut, code := run("rs", "diff", "-n", "kdiag-test", "-l", "app=does-not-exist")
	if code == 0 {
		t.Error("expected non-zero exit when selector matches no deployments")
	}
	if strings.TrimSpace(errOut) == "" {
		t.Errorf("expected error message in stderr, got nothing")
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

// ── helpers ───────────────────────────────────────────────────────────────────

func assertContainerInfo(t *testing.T, out string) {
	t.Helper()
	for _, want := range []string{"Container:", "State:", "Ready:", "Restart Count:"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output:\n%s", want, out)
		}
	}
}
