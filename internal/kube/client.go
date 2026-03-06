package kube

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type KubeFlags struct {
	Kubeconfig string
	Context    string
	Namespace  string
}

type KubeEnv struct {
	Clientset *kubernetes.Clientset
	Namespace string
}

func NewKubeEnv(k KubeFlags) (*KubeEnv, error) {
	cfgLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if k.Kubeconfig != "" {
		cfgLoadingRules.ExplicitPath = k.Kubeconfig
	}

	overrides := &clientcmd.ConfigOverrides{}
	if k.Context != "" {
		overrides.CurrentContext = k.Context
	}
	// Only override namespace if user explicitly provided it.
	if k.Namespace != "" {
		overrides.Context.Namespace = k.Namespace
	}

	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(cfgLoadingRules, overrides)

	ns, _, err := clientCfg.Namespace()
	if err != nil {
		return nil, fmt.Errorf("resolve namespace: %w", err)
	}
	if ns == "" {
		ns = "default"
	}

	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	return &KubeEnv{
		Clientset: cs,
		Namespace: ns,
	}, nil
}
