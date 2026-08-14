// Command grfdoctor is a console tool for diagnosing and fixing OpenTTD
// savegames whose vehicles reference NewGRFs that are no longer loaded.
//
// This is the CLI entry point. The interactive TUI (analyze/browse/match
// screens) lives alongside these subcommands and will become the default
// invocation once it's wired up; until then, `grfdoctor <subcommand>`
// covers the scriptable/CI-facing parts of the tool.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kolonuk/ottd-grf-doctor/internal/engine"
	"github.com/kolonuk/ottd-grf-doctor/internal/lint"
	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "analyze":
		err = runAnalyze(os.Args[2:])
	case "lint":
		err = runLint(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `grfdoctor - fix OpenTTD savegames with missing NewGRF vehicle references

Usage:
  grfdoctor analyze <savegame.sav>
      List every NewGRF the save references but doesn't have loaded, the
      EngineID slots it used, and how many vehicles are affected.

  grfdoctor lint <savegame.sav> [--openttd <path-to-binary>] [--timeout <seconds>]
      Validate a savegame's structural integrity (chunk framing, EIDS key
      uniqueness, VEHS engine references). With --openttd, also does a
      real headless load as a smoke test.

The interactive TUI for browsing replacement GRFs and building a fix plan
is not wired up yet -- see README.md for current status.
`)
}

func runAnalyze(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grfdoctor analyze <savegame.sav>")
	}
	s, err := sav.Load(args[0])
	if err != nil {
		return err
	}
	chunks, err := sav.WalkChunks(s.Payload)
	if err != nil {
		return err
	}
	cm := sav.ChunkMapOf(chunks)

	eids, err := engine.ParseEIDS(s.Payload, cm["EIDS"])
	if err != nil {
		return fmt.Errorf("parsing EIDS: %w", err)
	}
	ngrf, err := engine.ParseNGRF(s.Payload, cm["NGRF"])
	if err != nil {
		return fmt.Errorf("parsing NGRF: %w", err)
	}
	vehicles, err := engine.ParseVEHS(s.Payload, cm["VEHS"])
	if err != nil {
		return fmt.Errorf("parsing VEHS: %w", err)
	}

	an := engine.Analyze(eids, ngrf, vehicles)
	if len(an.Missing) == 0 {
		fmt.Println("No missing NewGRF references found -- every engine slot this save uses is either a default engine or a currently-loaded GRF.")
		return nil
	}

	fmt.Printf("Found %d missing NewGRF(s):\n\n", len(an.Missing))
	for _, m := range an.Missing {
		fmt.Printf("GRFID %s: %d engine slot(s), %d affected vehicle(s)\n", m.GRFID, len(m.Slots), len(m.Vehicles))
		fmt.Printf("  slots: %v\n", m.Slots)
	}
	return nil
}

func runLint(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: grfdoctor lint <savegame.sav> [--openttd <path>] [--timeout <seconds>]")
	}
	path := args[0]
	openttdBin := ""
	timeoutSec := 15
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--openttd":
			i++
			if i >= len(args) {
				return fmt.Errorf("--openttd requires a path")
			}
			openttdBin = args[i]
		case "--timeout":
			i++
			if i >= len(args) {
				return fmt.Errorf("--timeout requires a value")
			}
			fmt.Sscanf(args[i], "%d", &timeoutSec)
		default:
			return fmt.Errorf("unknown flag %q", args[i])
		}
	}

	report, err := lint.LintWithClient(context.Background(), path, openttdBin, time.Duration(timeoutSec)*time.Second)
	if err != nil {
		return err
	}
	for _, f := range report.Findings {
		fmt.Printf("[%s] %s\n", f.Severity, f.Message)
	}
	if report.HasErrors() {
		return fmt.Errorf("lint found errors")
	}
	return nil
}
