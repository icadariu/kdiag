// format.go
package main

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
)

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func printKVBlock(w io.Writer, indent string, m map[string]string) {
	if len(m) == 0 {
		fmt.Fprintf(w, "%s<none>\n", indent)
		return
	}
	for _, k := range sortedKeys(m) {
		fmt.Fprintf(w, "%s%s: %s\n", indent, k, m[k])
	}
}
