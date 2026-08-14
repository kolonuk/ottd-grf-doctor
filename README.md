# grfdoctor

A tool for OpenTTD savegames whose vehicles reference NewGRFs that are no
longer loaded (removed, blacklisted, or swapped for a replacement) --
they end up showing as generic default trains, or worse, can crash the
game on load if fixed incorrectly. `grfdoctor` fixes this directly in the
save file, and validates that the result won't crash before you load it
for real.

This project grew out of hand-fixing exactly this problem for one real
savegame; the hard-won knowledge from that process (especially two ways
to accidentally crash the game while "fixing" it) is encoded directly in
this package's doc comments and tests -- see `internal/engine/eids.go`
and `internal/engine/vehs.go` in particular.

## Status

Core engine (savegame parsing, analysis, safe remapping, NGRF
insertion/removal, structural linting) is implemented and covered by a
byte-exact regression test against a real fixed savegame. The CLI has
`analyze` and `lint` subcommands. The interactive TUI (browse the
official NewGRF catalog, match broken vehicles to replacement engines,
remove carriages, toggle detail views, build and apply a plan) is not
wired up yet -- that's the next piece of work.

## Why this is tricky

OpenTTD's savegame engine-ID table is keyed by `(GRFID, internal engine
ID)`, not by pool slot. Naively pointing several different broken engine
slots at the same replacement engine silently collides in that table --
only one of them ends up with a real backing object, and the rest become
holes in what OpenTTD assumes is a dense, gap-free pool. The game doesn't
notice until the *next* load, when it sorts the full engine list and
dereferences one of those holes: a hard crash, before the game even
reaches the menu, with no useful error message. `internal/engine`'s
`ValidateUniqueKeys` exists specifically to make this invariant
impossible to violate silently, and `ApplyToPayload` calls it before
writing anything to disk.

Separately, reassigning a vehicle that's the *rear half* of a
multiheaded-pair consist to a different, non-paired engine without also
clearing its subtype flags trips a different piece of OpenTTD's own
load-time logic (`ConnectMultiheadedTrains`) into mutating a vehicle in a
way it was never designed for. `internal/engine/doctor.go`'s
`findBrokenMultiheadPairs` detects and fixes this automatically.

## Usage

```sh
go build ./cmd/grfdoctor

./grfdoctor analyze mysave.sav      # list missing GRFs and affected vehicles
./grfdoctor lint mysave.sav         # structural validation
./grfdoctor lint mysave.sav --openttd /path/to/openttd --timeout 20
                                     # + a real headless load smoke test
```

## Testing

`testdata/<name>/{broken.sav,fixed.sav}` pairs are real savegame fixtures.
`go test ./...` discovers every pair automatically for a baseline
structural check; `internal/engine/doctor_test.go` additionally
reconstructs the exact fix for `xpressways-2082` using only this
package's public API and asserts the result matches `fixed.sav` byte for
byte at the decompressed-payload level (the two files' *compressed*
bytes will legitimately differ -- different LZMA encoders don't produce
identical output for identical input, and that's not a correctness
signal; what OpenTTD actually reads is the decompressed payload).

The `Test` GitHub Actions workflow (manual trigger only, `workflow_dispatch`)
additionally downloads the latest stable OpenTTD release and does a real
headless load of every fixture savegame, failing if OpenTTD reports a
fatal/corrupt-savegame error.

## Format reference

`internal/sav` implements OpenTTD's savegame container format (OTTX/LZMA
header, RIFF-style and gamma-length-prefixed chunk records) from scratch,
referencing OpenTTD's own `src/saveload/saveload.cpp`. `internal/engine`
interprets the specific chunks this tool cares about (`EIDS`, `ENGN`,
`NGRF`, `VEHS`) at savegame version 194 specifically -- see the doc
comments on `ParseVEHS` for what that means for supporting other
versions.
