package cli

import (
	"fmt"
	"io"
)

func PrintUsage(w io.Writer) {
	fmt.Fprint(w, `

Usage:
  kdiag inspect pod [flags] <pod_name>
  kdiag inspect pod [flags] -l <label_selector>
  kdiag az pods [flags] -l <label_selector>

Common flags:
  --kubeconfig <path>     Path to kubeconfig
  --context <name>        Kube context name
  -n, --namespace <ns>    Namespace (if omitted, uses namespace from current context like kubectl)

Inspect examples:
  kdiag inspect pod gateway-proxy-abc123
  kdiag inspect pod -n gloo-system gateway-proxy-abc123
  kdiag inspect pod -n gloo-system --resources gateway-proxy-abc123
  kdiag inspect pod -n gloo-system -l 'gateway-proxy-id=gateway-proxy,gateway-proxy=live'

AZ examples:
  kdiag az pods -n gloo-system -l 'gateway-proxy-id=gateway-proxy,gateway-proxy=live'

Notes:
  - "Zone/AZ" is derived from node labels:
      topology.kubernetes.io/zone
    fallback:
      failure-domain.beta.kubernetes.io/zone
`)
}
