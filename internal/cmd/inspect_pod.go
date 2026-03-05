package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"github.com/spf13/pflag"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

func RunInspect(args []string) {
	fs := pflag.NewFlagSet("inspect pod", pflag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVar(&k.Kubeconfig, "kubeconfig", "", "path to kubeconfig")
	fs.StringVar(&k.Context, "context", "", "kube context")
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (defaults to current context)")
	var showResources bool
	fs.BoolVar(&showResources, "resources", false, "show resource requests/limits")
	var selector string
	fs.StringVarP(&selector, "selector", "l", "", "label selector; omit to inspect all pods")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: kdiag inspect pod [flags] [<pod_name> | -l <selector>]")
		fmt.Fprintln(os.Stderr, "\nShow container state for one pod or a set of pods.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fmt.Fprint(os.Stderr, fs.FlagUsages())
	}

	if len(args) < 1 || args[0] != "pod" {
		fmt.Fprintln(os.Stderr, "Error: inspect requires subcommand: pod")
		fs.Usage()
		os.Exit(1)
	}

	_ = fs.Parse(args[1:])
	rest := fs.Args()
	selector = strings.TrimSpace(selector)

	// Pod name and selector are mutually exclusive.
	if len(rest) > 0 && selector != "" {
		fmt.Fprintln(os.Stderr, "Error: provide either <pod_name> OR --selector/-l (not both)")
		fs.Usage()
		os.Exit(1)
	}
	if len(rest) > 1 {
		fmt.Fprintln(os.Stderr, "Error: inspect pod accepts only one <pod_name>")
		os.Exit(1)
	}

	env, err := kube.NewKubeEnv(k)
	if err != nil {
		cli.Fatal(err)
	}
	ctx := context.Background()

	fmt.Printf("Namespace: %s\n", env.Namespace)

	// Single pod by name.
	if len(rest) == 1 {
		pod, err := env.Clientset.CoreV1().Pods(env.Namespace).Get(ctx, rest[0], kube.GetOptions())
		if err != nil {
			cli.Fatal(fmt.Errorf("get pod: %w", err))
		}
		inspectPodObject(*pod, showResources)
		return
	}

	// List by selector, or all pods when selector is empty.
	pods, err := env.Clientset.CoreV1().Pods(env.Namespace).List(ctx, kube.ListOptions(selector))
	if err != nil {
		cli.Fatal(fmt.Errorf("list pods: %w", err))
	}
	if len(pods.Items) == 0 {
		fmt.Println("No pods found.")
		return
	}
	for i := range pods.Items {
		p := pods.Items[i]
		fmt.Println("==========================================")
		fmt.Printf("Pod: %s\n", p.Name)
		inspectPodObject(p, showResources)
	}
}

func inspectPodObject(podObj corev1.Pod, showResources bool) {
	if len(podObj.Status.ContainerStatuses) == 0 {
		fmt.Printf("No containerStatuses found (pod may be Pending/Initializing)\n")
		return
	}

	for _, cs := range podObj.Status.ContainerStatuses {
		fmt.Printf("Container:       %s\n", cs.Name)
		fmt.Printf("  State:         %s\n", kube.ContainerStateKey(cs.State))
		if r := kube.ContainerStateReason(cs.State); r != "" {
			fmt.Printf("    Reason:      %s\n", r)
		}
		fmt.Printf("  Last State:    %s\n", kube.ContainerStateKey(cs.LastTerminationState))
		if r := kube.ContainerStateReason(cs.LastTerminationState); r != "" {
			fmt.Printf("    Reason:      %s\n", r)
		}
		fmt.Printf("  Ready:         %t\n", cs.Ready)
		fmt.Printf("  Restart Count: %d\n", cs.RestartCount)

		if showResources {
			req, lim := kube.ResourcesForContainer(podObj.Spec.Containers, cs.Name)
			fmt.Printf("  Resources:\n")
			fmt.Printf("    Requests:\n")
			cli.PrintKVBlock(os.Stdout, "      ", req)
			fmt.Printf("    Limits:\n")
			cli.PrintKVBlock(os.Stdout, "      ", lim)
		}
		fmt.Println()
	}
}
