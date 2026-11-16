package cmd

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/lmorchard/me-to-markdown/internal/sources"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Show registered exhaust sources and their auth status",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SLUG\tLABEL\tAUTH STATUS")
	_, _ = fmt.Fprintln(w, "----\t-----\t-----------")

	env := GetEnvMap()
	for _, s := range sources.All() {
		ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
		err := s.ValidateAuth(ctx, env)
		cancel()

		status := "✓ configured"
		if err != nil {
			status = fmt.Sprintf("needs auth (%v)", err)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name(), s.Label(), status)
	}
	return w.Flush()
}
