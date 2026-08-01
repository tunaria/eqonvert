# Contributing to eqonvert

Thanks for your interest! eqonvert is a reverse-engineered converter for EQOA
(PS2) assets. Contributions — bug reports, format findings, and code — are
welcome.

## Ground rules

- **No game assets.** Never commit `.esf`/`.csf`/`.bgm`/`.16`/`.pss`/`.iso`
  files, extracted output (`.glb`/`.flac`/`.vag`/`.png`), or ISOs. The
  `.gitignore` blocks these; keep it that way. This tool only *reads* files you
  already own.
- **Prove format claims.** Where a structure or offset is asserted, cite the
  evidence (a Ghidra decompile of the client, or cross-validation against real
  game data). See [docs/](docs/) for the style — especially
  [docs/ANIMATION.md](docs/ANIMATION.md), which documents the format's traps.

## Development

Requires Go 1.25+. ffmpeg + openmpt123 are optional (audio/video only).

```sh
go build -o eqonvert .   # build
go test ./...            # test
go vet ./...             # vet
gofmt -l .               # formatting (should print nothing)
```

CI runs build/vet/test on every push and PR.

## Working practice

These exist because each one has already cost real time on this codebase.

**Everything in progress lives on a branch.** Not in the working tree, not in
`git stash`. A stash is invisible to `git log`, does not survive a rebase, and is
easy to drop by accident. A branch is inspectable and cannot be lost silently. If
work is worth keeping until tomorrow, it is worth a branch and a WIP commit.

**One tree, one intent.** Do not let unrelated work accumulate in the same
working tree. It has happened here that a day of material fixes was built on a
worktree four commits behind `main`, so the export it produced silently lacked
the attachment parsing that had already landed. Check `git log main..HEAD` and
`git log HEAD..main` before starting, not after.

**Smallest change that solves the problem.** Prefer a threshold to a rewrite, and
a scoped default to a global one. Two defaults in `pkg/gltf/export.go` are
content-scoped for exactly this reason — the console-faithful `alphaCutoff` fixes
zone cutouts and shreds character faces, so it is 0.999 for zones and 0.5 for
characters rather than one value forced on both.

**Faithful is not automatically better.** Reproducing what the hardware does can
make the export worse, because glTF cannot express everything the GS could. When
that happens, record the finding, keep the decoded value in material `extras`,
and leave the behaviour off behind a flag. See `emitBlendFromMaterial` and
`SetSkinModulate`.

**Verify before recording a baseline.** `golden_test.go` fails loudly after any
intended change; that is the point. Read the diff and confirm the changed fields
are the ones you meant to change before running `-update`. A blind re-record
turns the test into a rubber stamp. Note that `go test` caches results — pass
`-count=1` when the inputs are export files outside the module, or you will read
a stale pass.

**Check the thing the user sees.** A GLB verified in isolation is not the same as
the app that loads it. A regression can live entirely in the consumer while the
export is correct.

## Pull requests

- Keep changes focused; one logical change per PR.
- Run `gofmt`, `go vet`, and `go test` before pushing.
- For parser changes, note how the behavior was validated (e.g. converted a
  known file and checked the output).
- Match the surrounding code's style and comment density.

## Reporting bugs

Open an issue with:
- what you ran (the exact `eqonvert …` command),
- what happened vs. what you expected,
- the build/game version (`eqonvert --version`) and your OS.

Please do **not** attach game assets — describe the file (type, size) instead.
