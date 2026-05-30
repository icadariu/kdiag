package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	"example.com/kdiag/internal/ai"
	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// RunTroubleshoot implements `kdiag troubleshoot <kind> [<name> | --label <sel>]`,
// a kind-aware diagnostic (pod scheduling/runtime, workload replica health, node
// health). Without --ai it prints text (or --yaml). With --ai it routes the
// collected report to an AI backend: bare --ai emits a paste-ready, read-only
// SRE prompt; --ai=<provider> selects a provider CLI (see internal/ai).
func RunTroubleshoot(args []string) {
	if cli.WantsHelp(args) {
		cli.PrintTroubleshootUsage(os.Stdout)
		return
	}

	fs := pflag.NewFlagSet("troubleshoot", pflag.ContinueOnError)
	fs.SortFlags = false
	var k kube.KubeFlags
	var selector string
	var asYAML bool
	var aiProvider string
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (defaults to current context)")
	fs.StringVarP(&selector, "label", "l", "", "label selector (pod, node)")
	fs.BoolVar(&asYAML, "yaml", false, "emit YAML instead of text")
	fs.StringVar(&aiProvider, "ai", "",
		"AI-assisted analysis: bare --ai prints a paste-ready read-only prompt; "+
			"--ai=claude|gemini|chatgpt launches that CLI")
	// Optional-value flag: bare `--ai` selects the default (prompt) backend; a
	// provider needs the `=` form (`--ai=claude`) since a space-separated value
	// would be read as the positional name.
	fs.Lookup("ai").NoOptDefVal = ai.DefaultProvider
	fs.Usage = func() { cli.PrintTroubleshootUsage(os.Stderr) }

	if err := fs.Parse(args); err != nil {
		cli.Fatal(err)
	}

	rest := fs.Args()
	if len(rest) < 1 {
		cli.Fatal(fmt.Errorf("troubleshoot requires a kind: pod, deploy, ds, sts, rs, node"))
	}
	kind := rest[0]
	var name string
	switch len(rest) {
	case 1:
		// kind only
	case 2:
		name = rest[1]
	default:
		cli.Fatal(fmt.Errorf("troubleshoot accepts only one name argument, got %d", len(rest)-1))
	}

	aiRequested := fs.Changed("ai")
	if aiRequested && asYAML {
		cli.Fatal(fmt.Errorf("--ai and --yaml are mutually exclusive (each selects a different output)"))
	}
	if name != "" && selector != "" {
		cli.Fatal(fmt.Errorf("provide either <name> or --label (not both)"))
	}
	// Validate the provider before any cluster I/O so a typo fails fast.
	if aiRequested {
		if _, ok := ai.Get(aiProvider); !ok {
			cli.Fatal(fmt.Errorf("unknown --ai provider %q (valid: %s)",
				aiProvider, strings.Join(ai.Providers(), ", ")))
		}
	}

	env, err := kube.NewKubeEnv(k)
	if err != nil {
		cli.Fatal(err)
	}
	runTroubleshoot(env, kind, name, selector, asYAML, aiProvider, aiRequested)
}

// runTroubleshoot collects the kind-appropriate report and either renders it
// (text/YAML) or feeds it to the selected AI backend.
func runTroubleshoot(env *kube.KubeEnv, kind, name, selector string, asYAML bool, aiProvider string, aiRequested bool) {
	ctx := context.Background()
	canonical := kube.CanonicalKind(kind)

	var report any
	var target, verdict string

	switch canonical {
	case "pod":
		reps := collectPodReports(env, ctx, name, selector)
		if !aiRequested {
			renderPodReports(reps, name, asYAML)
			return
		}
		report, target, verdict = listAIPayload(reps, name, selector)
	case "deployment", "daemonset", "statefulset", "replicaset":
		if name == "" {
			cli.Fatal(fmt.Errorf("troubleshoot %s requires a <name>", kind))
		}
		if selector != "" {
			cli.Fatal(fmt.Errorf("troubleshoot on a workload takes a <name>, not --label"))
		}
		rep := troubleshootWorkload(env, ctx, canonical, name)
		if !aiRequested {
			if asYAML {
				emit(rep)
			} else {
				printWorkloadTroubleshoot(os.Stdout, rep)
			}
			return
		}
		report, target, verdict = rep, name, rep.Verdict
	case "node":
		reps := collectNodeReports(env, ctx, name, selector)
		if !aiRequested {
			renderNodeReports(reps, name, asYAML)
			return
		}
		report, target, verdict = listAIPayload(reps, name, selector)
	default:
		cli.Fatal(fmt.Errorf("unknown troubleshoot kind: %s", kind))
	}

	runAIBackend(aiProvider, ai.Request{
		Kind:      canonical,
		Target:    target,
		Namespace: env.Namespace,
		Verdict:   verdict,
	}, report)
}

// listAIPayload reduces a slice of reports to the AI payload: a single named
// target becomes a scalar (with its verdict); anything else stays a list with a
// descriptive target and no single verdict. Works for pod and node report
// slices via any (the report is only ever marshaled to YAML downstream).
func listAIPayload[T podTroubleshoot | nodeTroubleshoot](reps []T, name, selector string) (any, string, string) {
	if len(reps) == 1 && name != "" {
		return reps[0], name, verdictOf(reps[0])
	}
	return reps, targetDesc(name, selector), ""
}

func verdictOf(v any) string {
	switch r := v.(type) {
	case podTroubleshoot:
		return r.Verdict
	case nodeTroubleshoot:
		return r.Verdict
	default:
		return ""
	}
}

func targetDesc(name, selector string) string {
	switch {
	case name != "":
		return name
	case selector != "":
		return "--label " + selector
	default:
		return "all in namespace"
	}
}

// runAIBackend marshals the report into the request and runs the named backend.
func runAIBackend(provider string, req ai.Request, report any) {
	backend, ok := ai.Get(provider)
	if !ok {
		cli.Fatal(fmt.Errorf("unknown --ai provider %q (valid: %s)",
			provider, strings.Join(ai.Providers(), ", ")))
	}
	y, err := yaml.Marshal(report)
	if err != nil {
		cli.Fatal(fmt.Errorf("marshal report: %w", err))
	}
	req.ReportYAML = string(y)
	if err := backend.Run(os.Stdout, req); err != nil {
		cli.Fatal(err)
	}
}
