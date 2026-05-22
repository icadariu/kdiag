package cmd

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"example.com/kdiag/internal/kube"
)

// RunComplete is the hidden helper invoked by shell completion scripts.
// It is dispatched from main.go's "__complete" case and is intentionally
// absent from PrintRootUsage.
//
// Subcommands:
//
//	kdiag __complete namespaces [<prefix>]
//	  → list namespaces (filtered by prefix), one per line.
//
//	kdiag __complete resources <kind> [<namespace>] [<prefix>]
//	  → list names of <kind> in <namespace>, one per line.
//	    <kind> is resolved via the cluster's discovery doc, so canonical
//	    names, plurals, kubectl shortnames (cm/svc/po/...) and CRDs all
//	    work.
//	    Cluster-scoped kinds (node, pv, ...) ignore <namespace>.
//	    Empty <namespace> falls back to the kubeconfig's current-context ns.
//
// All errors are silent — completion scripts redirect stderr to /dev/null,
// and a noisy failure (cluster down, bad kubeconfig) shouldn't pollute
// the user's shell. We just print nothing and exit 0.
func RunComplete(args []string) {
	if len(args) < 1 {
		return
	}
	switch args[0] {
	case "namespaces":
		var prefix string
		if len(args) > 1 {
			prefix = args[1]
		}
		completeNamespaces(prefix)
	case "resources":
		if len(args) < 2 {
			return
		}
		kind := args[1]
		var ns, prefix string
		if len(args) > 2 {
			ns = args[2]
		}
		if len(args) > 3 {
			prefix = args[3]
		}
		completeResources(kind, ns, prefix)
	}
}

func completeNamespaces(prefix string) {
	env, err := kube.NewKubeEnv(kube.KubeFlags{})
	if err != nil {
		return
	}
	list, err := env.Clientset.CoreV1().Namespaces().List(context.Background(), kube.ListOptions(""))
	if err != nil {
		return
	}
	for _, ns := range list.Items {
		if prefix == "" || strings.HasPrefix(ns.Name, prefix) {
			fmt.Println(ns.Name)
		}
	}
}

func completeResources(kind, ns, prefix string) {
	env, err := kube.NewKubeEnv(kube.KubeFlags{Namespace: ns})
	if err != nil {
		return
	}
	resolved, err := kube.ResolveResource(env.Mapper, kind)
	if err != nil {
		return
	}

	ctx := context.Background()
	ri := env.Dynamic.Resource(resolved.GVR)
	var list *unstructured.UnstructuredList
	if resolved.Namespaced {
		list, err = ri.Namespace(env.Namespace).List(ctx, kube.ListOptions(""))
	} else {
		list, err = ri.List(ctx, kube.ListOptions(""))
	}
	if err != nil {
		return
	}

	for _, item := range list.Items {
		name := item.GetName()
		if prefix == "" || strings.HasPrefix(name, prefix) {
			fmt.Println(name)
		}
	}
}
