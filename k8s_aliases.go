// k8s_aliases.go
package main

import corev1 "k8s.io/api/core/v1"

// Small alias to keep cmd_inspect_pod.go lighter; optional but keeps imports tidy.
type corePod = corev1.Pod
