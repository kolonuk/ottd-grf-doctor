// Command grfdoctor is a console tool for diagnosing and fixing OpenTTD
// savegames whose vehicles reference NewGRFs that are no longer loaded.
//
// This is the CLI entry point. `grfdoctor fix <savegame.sav>` launches
// the interactive TUI; `analyze` and `lint` cover the scriptable/CI-facing
// parts of the tool.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kolonuk/ottd-grf-doctor/internal/engine"
	"github.com/kolonuk/ottd-grf-doctor/internal/lint"
	"github.com/kolonuk/ottd-grf-doctor/internal/sav"
	"github.com/kolonuk/ottd-grf-doctor/internal/ui"
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
	case "fix":
		err = runFix(os.Args[2:])
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

  grfdoctor fix <savegame.sav>
      Launch the interactive TUI: browse broken GRFs, search and download
      replacements from the live BaNaNaS catalog, match broken vehicles to
      new engines (with carriage removal and non-blocking date/railtype
      warnings), then apply, lint, and save.
`)
}

func runFix(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grfdoctor fix <savegame.sav>")
	}
	m, err := ui.LoadModel(args[0])
	if err != nil {
		return err
	}
	return ui.NewApp(m).Run()
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
	otherVehicles, err := engine.ParseOtherVehicles(s.Payload, cm["VEHS"])
	if err != nil {
		return fmt.Errorf("parsing VEHS (road/ship/aircraft): %w", err)
	}
	obid, err := engine.ParseOBID(s.Payload, cm["OBID"])
	if err != nil {
		return fmt.Errorf("parsing OBID: %w", err)
	}
	objs, err := engine.ParseOBJS(s.Payload, cm["OBJS"])
	if err != nil {
		return fmt.Errorf("parsing OBJS: %w", err)
	}

	an := engine.Analyze(eids, ngrf, vehicles, otherVehicles, obid, objs)
	if len(an.Missing) == 0 && len(an.MissingObjects) == 0 {
		fmt.Println("No missing NewGRF references found -- every engine/object slot this save uses is either a default or a currently-loaded GRF.")
		return nil
	}

	if len(an.Missing) > 0 {
		fmt.Printf("Found %d missing vehicle NewGRF(s):\n\n", len(an.Missing))
		for _, m := range an.Missing {
			fmt.Printf("GRFID %s: %d engine slot(s), %d train(s), %d other vehicle(s)\n", m.GRFID, len(m.Slots), len(m.Vehicles), len(m.OtherVehicles))
			fmt.Printf("  slots: %v\n", m.Slots)
		}
	}
	if len(an.MissingObjects) > 0 {
		fmt.Printf("\nFound %d missing object/scenery NewGRF(s):\n\n", len(an.MissingObjects))
		for _, m := range an.MissingObjects {
			fmt.Printf("GRFID %s: %d object type slot(s), %d affected instance(s)\n", m.GRFID, len(m.Slots), len(m.Instances))
			fmt.Printf("  slots: %v\n", m.Slots)
		}
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
