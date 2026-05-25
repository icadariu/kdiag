package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"example.com/kdiag/internal/cli"
	"example.com/kdiag/internal/kube"
)

// Event messages occasionally contain newlines or tabs (probe stderr,
// operator-emitted payloads). Both characters are control codes for
// text/tabwriter — \n terminates the row, \t separates columns — so leaving
// them in would desync the table for every event that followed. Flatten
// them to spaces so each event renders as a single, well-aligned row.
var eventMessageReplacer = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")

func RunEvents(args []string) {
	fs := pflag.NewFlagSet("events", pflag.ExitOnError)
	var k kube.KubeFlags
	fs.StringVarP(&k.Namespace, "namespace", "n", "", "namespace (defaults to current context)")
	var allNamespaces bool
	fs.BoolVarP(&allNamespaces, "all-namespaces", "A", false, "list events across all namespaces (overrides --namespace)")
	var since time.Duration
	fs.DurationVar(&since, "since", time.Hour, "only show events newer than this duration (e.g. 30s, 5m, 2h)")
	fs.Usage = func() { printEventsHelp(os.Stderr, fs) }

	if cli.WantsHelp(args) {
		printEventsHelp(os.Stdout, fs)
		return
	}

	_ = fs.Parse(args)

	env, err := kube.NewKubeEnv(k)
	if err != nil {
		cli.Fatal(err)
	}

	ctx := context.Background()

	// -A overrides -n: list events across all namespaces.
	listNs := env.Namespace
	if allNamespaces {
		listNs = ""
	}

	evList, err := env.Clientset.CoreV1().Events(listNs).List(ctx, kube.ListOptions(""))
	if err != nil {
		cli.Fatal(fmt.Errorf("list events: %w", err))
	}

	cutoff := time.Now().Add(-since)
	type row struct {
		ts        time.Time
		namespace string
		evType    string
		reason    string
		object    string
		message   string
	}
	var rows []row
	for _, ev := range evList.Items {
		// Events created via events.k8s.io/v1 (e.g. kube-scheduler in 1.22+)
		// leave LastTimestamp zero and carry the time in EventTime or
		// Series.LastObservedTime. EffectiveEventTime picks the right field.
		ts := kube.EffectiveEventTime(ev)
		if ts.IsZero() {
			continue
		}
		if ts.Before(cutoff) {
			continue
		}
		rows = append(rows, row{
			ts:        ts,
			namespace: ev.Namespace,
			evType:    ev.Type,
			reason:    ev.Reason,
			object:    ev.InvolvedObject.Kind + "/" + ev.InvolvedObject.Name,
			message:   eventMessageReplacer.Replace(ev.Message),
		})
	}

	// Sort ascending so newest entry is last (like `kubectl logs`).
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ts.Before(rows[j].ts)
	})

	scope := "Namespace: " + env.Namespace
	if allNamespaces {
		scope = "Namespace: <all>"
	}
	fmt.Printf("%s\n\n", scope)

	if len(rows) == 0 {
		nsLabel := env.Namespace
		if allNamespaces {
			nsLabel = "<all>"
		}
		fmt.Printf("No events in namespace %s (last %s).\n", nsLabel, formatDuration(since))
		return
	}

	tw := cli.NewTabWriter(os.Stdout)
	if allNamespaces {
		fmt.Fprintln(tw, "AGE\tNAMESPACE\tTYPE\tREASON\tOBJECT\tMESSAGE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				cli.FormatAge(r.ts), r.namespace, r.evType, r.reason, r.object, r.message)
		}
	} else {
		fmt.Fprintln(tw, "AGE\tTYPE\tREASON\tOBJECT\tMESSAGE")
		for _, r := range rows {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				cli.FormatAge(r.ts), r.evType, r.reason, r.object, r.message)
		}
	}
	_ = tw.Flush()
}

// formatDuration renders a Duration cleanly: whole hours→"Xh", whole minutes→"Xm", else d.String().
func formatDuration(d time.Duration) string {
	if d >= time.Hour && d%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return d.String()
}

func printEventsHelp(w io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(w, "Usage: kdiag events [flags]")
	fmt.Fprintln(w, "\nShow events (Normal and Warning) in the current namespace.")
	fmt.Fprintln(w, "Events are sorted by their effective timestamp ascending (newest entry last, like `kubectl logs`).")
	fmt.Fprintln(w, "\nFlags:")
	fmt.Fprint(w, cli.FormatFlagsLongOnly(fs))
	fmt.Fprintln(w, "\nExamples:")
	fmt.Fprintln(w, "  kdiag events")
	fmt.Fprintln(w, "  kdiag events --all-namespaces --since 30m")
	fmt.Fprintln(w, "  kdiag events --namespace my-ns --since 24h")
}
