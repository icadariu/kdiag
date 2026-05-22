package cli

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"
)

func NewTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func PrintKVBlock(w io.Writer, indent string, m map[string]string) {
	if len(m) == 0 {
		fmt.Fprintf(w, "%s<none>\n", indent)
		return
	}
	for _, k := range sortedKeys(m) {
		fmt.Fprintf(w, "%s%s: %s\n", indent, k, m[k])
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// FormatAge returns a human-readable age string: <90s→Xs, <90m→Xm, <48h→Xh, else Xd.
func FormatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < 90*time.Second:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < 90*time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
