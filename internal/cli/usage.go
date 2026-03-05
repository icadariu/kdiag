package cli

import (
	"fmt"
	"io"
)

func PrintUsage(w io.Writer) {
	fmt.Fprint(w, `kdiag — Kubernetes diagnostic CLI

Usage:
  kdiag inspect pod [flags] [<pod_name> | -l <selector>]
  kdiag az pods     [flags] -l <selector>
  kdiag rs diff     [flags] [<deployment-name> | -l <selector>]

inspect pod — show container state for one pod or a set of pods
  --kubeconfig <path>         Path to kubeconfig file
  --context <name>            Kubernetes context to use
  --resources                 Also show CPU/memory requests and limits
  -n, --namespace <ns>        Namespace (defaults to current context)
  -l, --selector <expr>       Label selector; omit to inspect all pods

az pods — show pod placement across nodes and availability zones
  --kubeconfig <path>         Path to kubeconfig file
  --context <name>            Kubernetes context to use
  -n, --namespace <ns>        Namespace (defaults to current context)
  -l, --selector <expr>       Label selector (required)

rs diff — diff pod template spec between previous and current replicaset
  --kubeconfig <path>         Path to kubeconfig file
  --context <name>            Kubernetes context to use
  -n, --namespace <ns>        Namespace (defaults to current context)
  -l, --selector <expr>       Label selector to identify the deployment

Examples:
  kdiag inspect pod my-pod-abc123
  kdiag inspect pod -n kube-system my-pod-abc123
  kdiag inspect pod -n kube-system --resources my-pod-abc123
  kdiag inspect pod -l 'app=my-app,env=prod'
  kdiag az pods -n kube-system -l 'app=my-app'
  kdiag rs diff my-deployment
  kdiag rs diff -n my-ns -l 'app=my-app'

Notes:
  Zone/AZ is read from node labels (in order):
    topology.kubernetes.io/zone
    failure-domain.beta.kubernetes.io/zone  (legacy fallback)
`)
}
