package kube

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

type KubeFlags struct {
	Namespace string
}

type KubeEnv struct {
	Clientset *kubernetes.Clientset
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
	Mapper    meta.RESTMapper
	Namespace string
}

func NewKubeEnv(k KubeFlags) (*KubeEnv, error) {
	cfgLoadingRules := clientcmd.NewDefaultClientConfigLoadingRules()

	overrides := &clientcmd.ConfigOverrides{}
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

	dyn, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	// Cached discovery + deferred mapper: discovery is consulted lazily on
	// the first RESTMapper lookup, so commands that never touch the mapper
	// (inspect, diff) pay nothing extra. Wrapping in a ShortcutExpander
	// makes kubectl-style shortnames ("cm", "svc", "ing") resolve to their
	// canonical resources via the live discovery doc.
	cachedDisc := memory.NewMemCacheClient(cs.Discovery())
	baseMapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDisc)
	mapper := restmapper.NewShortcutExpander(baseMapper, cachedDisc, nil)

	return &KubeEnv{
		Clientset: cs,
		Dynamic:   dyn,
		Discovery: cachedDisc,
		Mapper:    mapper,
		Namespace: ns,
	}, nil
}
