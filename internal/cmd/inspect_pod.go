package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

func RunInspect(args []string) {
	if len(args) < 1 || args[0] != "pod" {
		fmt.Fprintln(os.Stderr, "Error: inspect requires subcommand: pod")
		cli.PrintUsage(os.Stderr)
		os.Exit(1)
	}

	fs := flag.NewFlagSet("inspect pod", flag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVar(&k.Kubeconfig, "kubeconfig", "", "path to kubeconfig")
	fs.StringVar(&k.Context, "context", "", "kube context")
	fs.StringVar(&k.Namespace, "namespace", "", "namespace")
	fs.StringVar(&k.Namespace, "n", "", "namespace (shorthand)")
	var showResources bool
	fs.BoolVar(&showResources, "resources", false, "show resource requests/limits")
	var selector string
	fs.StringVar(&selector, "selector", "", "label selector (inspect many pods)")
	fs.StringVar(&selector, "l", "", "label selector (inspect many pods, shorthand)")

	_ = fs.Parse(args[1:])
	rest := fs.Args()
	selector = strings.TrimSpace(selector)

	// Enforce either pod name OR selector.
	if (len(rest) == 0 && selector == "") || (len(rest) > 0 && selector != "") {
		fmt.Fprintln(os.Stderr, "Error: provide either <pod_name> OR --selector/-l (not both)")
		cli.PrintUsage(os.Stderr)
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

	if selector != "" {
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
		return
	}

	podName := rest[0]
	pod, err := env.Clientset.CoreV1().Pods(env.Namespace).Get(ctx, podName, kube.GetOptions())
	if err != nil {
		cli.Fatal(fmt.Errorf("get pod: %w", err))
	}
	inspectPodObject(*pod, showResources)
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
