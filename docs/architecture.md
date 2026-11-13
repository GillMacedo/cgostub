# Architecture

cgostub is one Go program in `plugin/scripts`, package `main`, with no dependencies outside the standard library. The skill builds it from the installed plugin directory, which has no `go.mod`.

## Flow for one target

1. **List.** `goList` runs `go list -e -deps -json=<fields>` for the patterns twice, with `CGO_ENABLED=1` and `CGO_ENABLED=0`, and `GOOS`, `GOARCH` and `-tags` set. Anything on stderr, an exit code other than 0, unreadable JSON or no matching package is a refusal.
2. **Check what was listed.** In the build with cgo, any package error is a refusal. In the build without cgo, the only accepted error is "build constraints exclude all Go files" (Go 1.22 says "no Go files in") for a package that has cgo files in the other build; it becomes `needs-cgo`. Dependency errors are accepted only when they come from such a package.
3. **Parse.** Every file of every package outside the standard library, in both builds, is parsed with `go/parser`. `go list` reads only the package clause and imports, so this catches broken files that a build would never see, such as a cut-off file that only the cgo build compiles. Files of the main module must end with a line break.
4. **Build.** When the target is the host and `go env CGO_ENABLED` is 1, `go build` runs with cgo; a failure is a refusal. `go build` then runs without cgo, unless step 2 already found a package that can't build. Output goes to a temporary directory (`-o dir/` when a main package is named; no `-o` otherwise, which writes nothing). Each failing package must be explained by `cgoCause`: an `undefined:` or `has no field or method` name that is declared only in the cgo-only files of the package or of a package it imports, or an error in a file compiled only without cgo. Otherwise it's a refusal.
5. **Compare.** `fileDiff` compares `GoFiles` plus `CgoFiles` in the two builds. Files only in the build without cgo are read by `findStub`; files only in the build with cgo are read by `findInits`. The result is `needs-cgo`, `stub`, `fallback` or `cgo-dropped`.
6. **Standard library.** Standard library packages become notes, with text from `stdExplain` for `net` and `os/user`. `plugin`, when it changes, is an error. `runtime/cgo` is skipped.

`importChains` finds the shortest import path from the named main packages to each package, for the `imported by` line.

## Output

Text output lists errors and warnings per target, then a summary line per target with the counts, `go env CGO_ENABLED` for that target in the user's environment, the standard library packages that change, and whether either build was skipped. JSON output has the same data, plus the file lists. Exit 2 always means stdout is empty.

## Decisions

See `docs/decisions/`.
