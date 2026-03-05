package cmd

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "sort"
  "strconv"

  "github.com/spf13/pflag"
  metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
  appsv1 "k8s.io/api/apps/v1"
  "sigs.k8s.io/yaml"

  "example.com/kdiag/internal/cli"
  "example.com/kdiag/internal/kube"
)

const revisionAnnotation = "deployment.kubernetes.io/revision"

func RunRS(args []string) {
  if len(args) < 1 || args[0] != "diff" {
    fmt.Fprintln(os.Stderr, "Error: rs requires subcommand: diff")
    cli.PrintUsage(os.Stderr)
    os.Exit(1)
  }

  fs := pflag.NewFlagSet("rs diff", pflag.ExitOnError)
  var k kube.KubeFlags
  fs.StringVar(&k.Kubeconfig, "kubeconfig", "", "path to kubeconfig")
  fs.StringVar(&k.Context, "context", "", "kube context")
  fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace")
  var selector string
  fs.StringVarP(&selector, "selector", "l", "", "label selector to find the deployment")
  _ = fs.Parse(args[1:])

  rest := fs.Args()
  if len(rest) == 0 && selector == "" {
    fmt.Fprintln(os.Stderr, "Error: rs diff requires <deployment-name> or -l <selector>")
    cli.PrintUsage(os.Stderr)
    os.Exit(1)
  }
  if len(rest) > 0 && selector != "" {
    fmt.Fprintln(os.Stderr, "Error: provide either <deployment-name> OR --selector/-l (not both)")
    cli.PrintUsage(os.Stderr)
    os.Exit(1)
  }

  env, err := kube.NewKubeEnv(k)
  if err != nil {
    cli.Fatal(err)
  }
  ctx := context.Background()

  var deploy appsv1.Deployment
  if len(rest) == 1 {
    d, err := env.Clientset.AppsV1().Deployments(env.Namespace).Get(ctx, rest[0], kube.GetOptions())
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

  if len(items) < 2 {
    fmt.Fprintln(os.Stderr, "Error: need at least 2 replicasets to diff (only one revision found)")
    os.Exit(1)
  }

  curr := items[0]
  prev := items[1]
  currRev := curr.Annotations[revisionAnnotation]
  prevRev := prev.Annotations[revisionAnnotation]

  prevYAML, err := yaml.Marshal(prev.Spec.Template.Spec)
  if err != nil {
    cli.Fatal(fmt.Errorf("marshal previous: %w", err))
  }
  currYAML, err := yaml.Marshal(curr.Spec.Template.Spec)
  if err != nil {
    cli.Fatal(fmt.Errorf("marshal current: %w", err))
  }

  prevFile, err := os.CreateTemp("", "kdiag-prev-*.yaml")
  if err != nil {
    cli.Fatal(err)
  }
  defer os.Remove(prevFile.Name())

  currFile, err := os.CreateTemp("", "kdiag-curr-*.yaml")
  if err != nil {
    cli.Fatal(err)
  }
  defer os.Remove(currFile.Name())

  if _, err := prevFile.Write(prevYAML); err != nil {
    cli.Fatal(err)
  }
  prevFile.Close()
  if _, err := currFile.Write(currYAML); err != nil {
    cli.Fatal(err)
  }
  currFile.Close()

  fmt.Printf("Deployment: %s/%s\n", env.Namespace, deploy.Name)
  fmt.Printf("Diff: revision %s → %s\n\n", prevRev, currRev)

  diffCmd := exec.Command("diff", "--color=always", "-u",
    "--label", fmt.Sprintf("revision/%s (%s)", prevRev, prev.Name),
    "--label", fmt.Sprintf("revision/%s (%s)", currRev, curr.Name),
    prevFile.Name(), currFile.Name())
  diffCmd.Stdout = os.Stdout
  diffCmd.Stderr = os.Stderr
  if err := diffCmd.Run(); err != nil {
    if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
      // exit code 1 means differences found — that's normal for diff
      return
    }
    cli.Fatal(fmt.Errorf("diff: %w", err))
  }
}
