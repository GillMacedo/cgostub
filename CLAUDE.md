# cgostub

Claude Code skill and Go CLI that reports the Go packages that change when a program is built with `CGO_ENABLED=0` instead of with cgo: stubs, fallbacks, cgo code left out and packages that don't build.

## Architecture
`plugin/scripts/cgostub.go` holds the CLI, the flags, the text and JSON output and the exit codes. `plugin/scripts/check.go` holds `checkTarget`, which lists the packages both ways, parses every file, runs the builds, compares the files each package compiles (`fileDiff`), and classifies the change (`findStub`, `findInits`, `cgoCause`, `stdExplain`). `plugin/scripts/golist.go` runs `go list`, `go env` and `go build` and reads their output. `plugin/skills/checking-go-builds-without-cgo/SKILL.md` tells Claude how to use it. See `docs/architecture.md`.

## Commands
- `make test`: gofmt, vet and tests, including `tests/`, which run the binary on the example modules, real standard library packages and broken input, and build and run the examples with and without cgo
- `make lint`: plugin manifest validation (needs Claude Code)
- `make demo`: run on the examples
- `make realworld`: run on real modules and projects (needs network)

## Conventions
- Standard library only, and no `go.mod` requirements: the installed plugin has no module.
- Any input it can't fully read or trust exits 2 with nothing on stdout, whatever `--fail-on` says. Add a case to `tests/inputs_test.go` for each new kind of input.
- A `needs-cgo` finding from a build failure must be explained by `cgoCause`. If the errors don't point at cgo, refuse; never report a cgo finding for a build that is broken for another reason.
- Never write outside the temporary directory, and never change the user's files or go.mod.
- Tests must not assume the machine they run on: use `host()` and `cross()` in `tests/main_test.go`, and skip what needs a C compiler with `hostCgo`.
- README `$ ` lines use cross targets, so their output doesn't depend on the machine or the Go version.

## Do and don't
- Do run `make test` on Go 1.22 and on the current Go release before calling a change done. go list error messages differ between them.
- Don't print "nothing changes" for a run that checked nothing.
- Don't claim behavior the code doesn't check: cgostub compares files, it doesn't run programs.

## Definition of done
`make test` and `make lint` pass, README output matches a real run, and `ROADMAP.md` and docs are updated when relevant.
