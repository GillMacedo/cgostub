---
name: checking-go-builds-without-cgo
description: Finds the Go packages that change when a program is built with CGO_ENABLED=0 instead of with cgo, for each GOOS/GOARCH target. Reports dependencies that compile a stub in place of their cgo code (such as go-sqlite3's "Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub"), switch to a different pure Go implementation, leave out cgo files and their init functions with nothing in their place, or don't build at all, and what the standard library's net, os/user and plugin packages do differently. Use when a release binary, Docker image or cross-compiled build works locally but fails at run time, when someone sets CGO_ENABLED=0 for a static or distroless build, when adding release targets to GoReleaser or a CI matrix, when a build in an image without a C compiler behaves differently, or when adding a dependency that might use cgo.
---

# Checking what changes when a Go program is built without cgo

cgostub runs `go list -deps` for the same packages with `CGO_ENABLED=1` and `CGO_ENABLED=0`, for each target, and compares the files each package compiles. It then runs a real `go build` without cgo to confirm what fails, and, when the target is this machine and Go found a C compiler, a real build with cgo to make sure the program builds at all. It needs Go 1.22 or newer and the program's source with its dependencies (module cache, `vendor/` or network). It doesn't run the program and doesn't change any files.

## Find out what to check

1. **The packages:** the main packages that get shipped, such as `./cmd/server`, or `./...` for the whole module. Run it from the module root, or pass `--dir`.
2. **The targets:** where the binaries are built without cgo. Look for `CGO_ENABLED=0` in the Dockerfile, the Makefile, `.goreleaser.yaml` (`env:` and `goos`/`goarch`), and the CI workflow. Cross-compiling turns cgo off unless `CGO_ENABLED=1` is set, and so does a build where Go finds no C compiler (an Alpine or distroless builder without gcc), so builds for other platforms usually count. Pass them with `--target linux/amd64,darwin/arm64,windows/amd64`. Without `--target`, it checks `go env GOOS/GOARCH`. When the release ships the usual set, `--target release` stands for `linux/amd64,linux/arm64,darwin/amd64,darwin/arm64,windows/amd64`, and it can be one entry of a list (`--target release,linux/386`).
3. **Build tags:** if the real build uses `-tags`, pass the same with `--tags`.

## Run it

Build it once, then run it:

    go build -o "${TMPDIR:-/tmp}/cgostub" $(ls "${CLAUDE_PLUGIN_ROOT}"/scripts/*.go | grep -v _test.go)
    "${TMPDIR:-/tmp}/cgostub" --target linux/amd64,darwin/arm64 ./cmd/server

Add `--std` to list the standard library packages that change, `--format json` for machine-readable output, and `--fail-on warn` to fail on warnings too.

## Read the exit code first

- **0:** no finding at or above `--fail-on` (default `error`). Warnings and notes may still be printed; read them.
- **1:** findings at or above `--fail-on`.
- **2:** nothing was checked. The message says why: `go list` or `go build` failed for a reason that isn't cgo (a syntax or type error, a missing module, a pattern that matches nothing, an unknown target), a file of the user's module doesn't end with a line break and may be cut off, or the program doesn't build even with cgo. Fix the input and run again. Never describe exit 2 as a clean result.

## Check each finding

- **ERROR `stub`:** without cgo the package compiles other files, and they look like a stub: a string that mentions cgo or says something is not implemented or supported, a `panic` call, or a name such as `ErrNotImplemented`. The evidence line quotes it. Open the file and confirm which functions the program calls.
- **ERROR `needs-cgo`:** the package doesn't build without cgo. The message quotes `go build` or says build constraints exclude all its files.
- **WARN `fallback`:** a different implementation is compiled in. It may behave the same (a pure Go version of the same algorithm) or not. Read both files and say which.
- **WARN `cgo-dropped`:** cgo files are left out with nothing in their place. If the message names an `init` function, whatever it registered (a `database/sql` driver, an image format, a codec) is missing at run time.
- **note `standard-library`** (with `--std`): `net` always uses the pure Go DNS resolver and `os/user` reads only `/etc/passwd` and `/etc/group` on Linux and similar systems. These matter when the service relies on NSS sources such as LDAP or SSSD.

## Report

1. For each target, the errors first: the package, the import chain from the program (`imported by`), the evidence, and whether the program calls the affected code.
2. Then the warnings, and whether each matters for this program.
3. The fix options: build that target with `CGO_ENABLED=1` and a C compiler (a cross compiler such as zig or a native runner for other platforms), use a pure Go alternative, or drop the target. Don't recommend a specific replacement library without checking it.
4. Say what wasn't checked: targets not named, whether the build with cgo was run (the summary line says so), and that a clean result covers build-time differences, not behavior of the code that is the same in both builds.

## Don't

Don't change the user's build files or dependencies unless they ask. Don't say the program works without cgo because cgostub found nothing: it compares which files are compiled, it doesn't run the program.
