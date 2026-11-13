# cgostub: find Go dependencies that turn into stubs with CGO_ENABLED=0

[![CI](https://github.com/GillMacedo/cgostub/actions/workflows/ci.yml/badge.svg)](https://github.com/GillMacedo/cgostub/actions/workflows/ci.yml) [![MIT license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**A Claude Code skill and Go CLI for teams that ship Go binaries built with `CGO_ENABLED=0`: static Docker images, distroless builders, GoReleaser and other cross-compiled releases. It compares what each dependency compiles with and without cgo, for each target, and reports the packages that become stubs, switch to other code, drop their cgo files and `init` functions, or stop building.**

A Go program that works with `go run` can break once it's built for release. Cross-compiling turns cgo off unless `CGO_ENABLED=1` is set, and so does a build where Go finds no C compiler. Many packages still build without cgo, because they ship a second file for that case. With `mattn/go-sqlite3`, that file is a stub, and every query fails at run time with `Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub`. GitHub has hundreds of issues quoting that message. Other packages compile a different implementation, or leave out a cgo file whose `init` function registered a driver. The build passes, the tests pass on a developer machine with cgo, and the problem shows up in production.

cgostub runs `go list -deps` for your packages with `CGO_ENABLED=1` and `CGO_ENABLED=0` for each target, compares the files every package compiles, and reads the files that only the build without cgo compiles. It confirms what fails with a real `go build` without cgo. When the target is the machine it runs on and Go has a C compiler, it also builds with cgo, so a program that doesn't build with cgo either is refused.

## What you can ask Claude

- "Our Docker image builds with CGO_ENABLED=0. Does any dependency stop working?"
- "We're adding darwin/arm64 and windows/amd64 to the GoReleaser config. What changes in those builds?"
- "The release binary says go-sqlite3 requires cgo to work. Which other packages have the same problem?"
- "Does this new dependency use cgo, and what happens to it in our static build?"

## Install

As a Claude Code plugin:

    claude plugin marketplace add GillMacedo/cgostub
    claude plugin install cgostub@cgostub-marketplace

Or run it with Go 1.22 or newer, from the module of the program you're checking. It has no dependencies outside the standard library:

    go run github.com/GillMacedo/cgostub/plugin/scripts@v0.1.0 --target linux/amd64,darwin/arm64,windows/amd64 ./cmd/server

## Example

`examples/photosvc` is a made-up service with three packages that use cgo. `heic` has a stub for builds without cgo, `checksum` has a pure Go version of its C code, and `thumbdb` registers a `database/sql` driver from a cgo file. Checked for a Windows release built on Linux, which has cgo off by default. `--fail-on never` keeps the exit code at 0 for this demo; without it, the run exits 1:

    $ go run ./plugin/scripts --dir examples/photosvc --target windows/amd64 --fail-on never ./...
    WARN fallback: example.com/photosvc/checksum, for windows/amd64
      without cgo it compiles sum_purego.go in place of 1 file that uses cgo (sum_cgo.go). It is a different implementation, so check that it behaves the same before you ship a build without cgo
      imported by example.com/photosvc

    ERROR stub: example.com/photosvc/heic, for windows/amd64
      without cgo it compiles decode_nocgo.go in place of 1 file that uses cgo (decode_cgo.go). That looks like a stub: decode_nocgo.go:7 has "heic: decoding needs cgo, and this binary was built with CGO_ENABLED=0"
      imported by example.com/photosvc
      fix: build windows/amd64 with CGO_ENABLED=1 and a C compiler for it, or use a package that doesn't need cgo

    WARN cgo-dropped: example.com/photosvc/thumbdb, for windows/amd64
      without cgo it leaves out 1 file that uses cgo (register_cgo.go), with nothing in its place, so the init function in register_cgo.go:10 never runs
      imported by example.com/photosvc

    windows/amd64: 1 error and 2 warnings in 4 packages outside the standard library. go env CGO_ENABLED for windows/amd64 here is 0.
      Built without cgo only: a build with cgo for windows/amd64 needs a C compiler for that target, so it wasn't tried.

These findings match what the binaries do. Built and run on Linux, the cgo build prints `heic: 40 pixels`, checksum `94739e27` and drivers `[thumbs]`; the build with `CGO_ENABLED=0` prints the heic error, the same checksum, and no drivers. `tests/examples_test.go` checks this on every run where a C compiler is available.

`examples/sensord` has a package with only cgo files, so it can't build without cgo:

    $ go run ./plugin/scripts --dir examples/sensord --target windows/amd64 --fail-on never ./...
    ERROR needs-cgo: example.com/sensord/lmsensors, for windows/amd64
      it doesn't build without cgo: build constraints exclude all of its Go files
      imported by example.com/sensord
      fix: build windows/amd64 with CGO_ENABLED=1 and a C compiler for it, or use a package that doesn't need cgo

    windows/amd64: 1 error and 0 warnings in 2 packages outside the standard library. go env CGO_ENABLED for windows/amd64 here is 0.
      The build without cgo wasn't run, because go list already shows a package that can't build. Compile errors in other packages may be hidden.

A pure Go service stays quiet, for several targets at once:

    $ go run ./plugin/scripts --dir examples/healthy --target windows/amd64,windows/arm64 .
    windows/amd64: nothing outside the standard library changes without cgo (1 package checked). go env CGO_ENABLED for windows/amd64 here is 0.
      Built without cgo only: a build with cgo for windows/amd64 needs a C compiler for that target, so it wasn't tried.
    windows/arm64: nothing outside the standard library changes without cgo (1 package checked). go env CGO_ENABLED for windows/arm64 here is 0.
      Built without cgo only: a build with cgo for windows/arm64 needs a C compiler for that target, so it wasn't tried.

With a real dependency, from `scripts/realworld.sh` (this one needs the network to download go-sqlite3 v1.14.24):

    ERROR stub: github.com/mattn/go-sqlite3, for linux/amd64
      without cgo it compiles static_mock.go in place of 10 files that use cgo (backup.go, callback.go, error.go and 7 more) and 3 files built only with cgo (sqlite3_go18.go, sqlite3_opt_preupdate.go, sqlite3_opt_preupdate_omit.go). That looks like a stub: static_mock.go:17 has "Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work. This is a stub"
      imported by example.com/sqlite3
      fix: build linux/amd64 with CGO_ENABLED=1 and a C compiler for it, or use a package that doesn't need cgo

The standard library changes too. On Linux, `--std` explains `net` and `os/user`:

    note standard-library: net, for linux/amd64
      net can't use the C library's resolver, so name lookups always use the pure Go resolver, which reads /etc/hosts and queries the DNS servers in /etc/resolv.conf. Other NSS sources in /etc/nsswitch.conf, such as LDAP or mDNS, are not used
      imported by example.com/healthy -> net/http

Add `--format json` for machine-readable output, with the files compiled only with cgo and only without it for each finding.

## How it works

1. `go list -e -deps` runs twice for each target, with `CGO_ENABLED=1` and `CGO_ENABLED=0`, and the same `GOOS`, `GOARCH` and `--tags`. Listing doesn't need a C compiler, so any target can be checked from any machine.
2. Every file of every package outside the standard library is parsed, in both builds, so a broken file that only one build compiles can't hide.
3. For each package, it compares the files the two builds compile. Files compiled only without cgo are read for signs of a stub: a string that mentions cgo or says something is not implemented or supported, a `panic` call, or a name such as `ErrNotImplemented`. Files dropped without a replacement are read for `init` functions.
4. `go build` runs without cgo, with its output in a temporary directory. A compile error counts as `needs-cgo` only when it points at cgo: a name declared only in files that need cgo, or an error in a file compiled only without cgo. Any other error is refused.
5. When the target is this machine and `go env CGO_ENABLED` is 1, `go build` also runs with cgo. If that fails, the program is broken anyway and cgostub refuses.

## When it refuses

cgostub exits 2 and prints only the reason when it can't trust what it read, whatever `--fail-on` says. For example, a package whose file is the output of a failed build instead of Go source:

    cgostub: can't trust the input, nothing was checked: darwin/arm64: example.com/photosvc/heic doesn't load for darwin/arm64 with CGO_ENABLED=1: heic/heic.go:1:1: illegal character U+0023 '#'

It does the same for empty, binary, garbage or cut-off files, in a common file, a file that uses cgo, a file built only without cgo, or the main package; for a file of your module that doesn't end with a line break (gofmt always adds one, so it may be cut off); for type errors that have nothing to do with cgo; for missing imports, modules that aren't in go.mod or can't be downloaded, an empty or broken go.mod, patterns that match nothing, a log file given as a package, an unknown target, and `go` missing from `PATH`; and for a program that doesn't build even with cgo, when that build can run.

A file cut on a line break can still be valid Go. If the cut removes the `import "C"` line or the build constraint, the package really doesn't change any more, and no tool can tell that it was cut. The summary line says how many packages were checked.

## What it reports

| Code | Severity | Meaning |
|---|---|---|
| `stub` | ERROR | Without cgo the package compiles other files, and they look like a stub. The evidence names the file and line. `plugin` from the standard library is reported this way when it changes |
| `needs-cgo` | ERROR | The package doesn't build without cgo: build constraints exclude all its files, or `go build` fails on a name declared only in cgo files |
| `fallback` | WARN | A different implementation is compiled in, with no sign of a stub. It may behave the same or not |
| `cgo-dropped` | WARN | Files that need cgo are left out with nothing in their place. The message names any `init` function that no longer runs |
| `standard-library` | note | A standard library package changes. `net` and `os/user` have an explanation for Linux and similar systems. Shown with `--std`, and named in the summary otherwise |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Nothing at or above `--fail-on`, which is `error` by default. Use `--fail-on warn` to fail on warnings too, or `never`. |
| 1 | Findings at or above `--fail-on`. |
| 2 | The input can't be trusted or the flags are wrong. Nothing was checked, and stdout is empty. |

## Limits

- **Files, not behavior.** cgostub compares which files are compiled. It doesn't run the program, so it can't tell whether a `fallback` behaves the same, and a clean result says nothing about code that is the same in both builds.
- **The stub check is a heuristic.** It reads the replacement files for the signs listed above. A stub that returns zero values without an error or a panic is reported as a `fallback`, and a real implementation that returns an "unsupported" error for some inputs is reported as a `stub`. The evidence line shows what it found.
- **The build with cgo runs only for this machine's target**, and only when Go found a C compiler. For other targets, a type error in a file that only the cgo build compiles isn't seen. Syntax errors are always seen.
- **When go list already shows a package that can't build without cgo**, the build without cgo isn't run, so compile errors in other packages may be hidden until that one is fixed. The summary says so.
- **One build configuration per run.** Pass the tags and targets the real build uses. It doesn't read Dockerfiles, Makefiles or `.goreleaser.yaml`.
- **Test files aren't checked.** Only the packages the program builds.
- **It builds your code.** `go build` compiles your packages and, for the build with cgo, the C code in their cgo files, as any build of the program would. It never runs the result. Use it on code you would build anyway.

## FAQ

**Why does my binary say "go-sqlite3 requires cgo to work"?** It was built with `CGO_ENABLED=0`, so go-sqlite3 compiled its stub, `static_mock.go`, instead of SQLite. Build with `CGO_ENABLED=1` and a C compiler for the target, or use a SQLite driver written in Go. cgostub finds this and any other package that does the same before you ship.

**When is cgo turned off without me asking?** When `CGO_ENABLED=0` is set, when cross-compiling (`GOOS` or `GOARCH` differs from the machine) without `CGO_ENABLED=1`, and since Go 1.20, when Go can't find a C compiler, as in many minimal build images. The summary line prints `go env CGO_ENABLED` for each target in your environment.

**Does it need a C compiler or Docker?** No. Listing and the build without cgo work for any target without one. With a C compiler, it also confirms the program builds with cgo on this machine.

**Does it change my files or go.mod?** No. It runs `go list` and `go build`, and build output goes to a temporary directory. With the default `-mod=readonly` the go command doesn't touch `go.mod` or `go.sum`; if your `GOFLAGS` has `-mod=mod`, the go command may update them, as it would for any build. An empty module cache is filled as usual, and the `go: downloading` lines are ignored.

**Is it an MCP server?** No. It's a skill with one Go program, so `go run` or `go build` is all it needs.

## Tested on

- **Real behavior:** `tests/examples_test.go` builds `examples/photosvc` with and without cgo, runs both binaries, and checks that the stub returns its error, the fallback gives the same checksum, and the driver registered from a cgo file is missing without cgo. It checks that `examples/sensord` really fails to build without cgo.
- **Real standard library packages:** a program that imports `net`, `os/user` and `plugin`, checked for linux/amd64: `plugin` is reported as a stub, `net` and `os/user` as notes, and notes never fail a run.
- **Real modules, by hand with `scripts/realworld.sh`:** `mattn/go-sqlite3` v1.14.24 is a `stub` for linux/amd64, darwin/arm64 and windows/amd64, and a binary built with `CGO_ENABLED=0` on Linux printed the stub error when run. For darwin/arm64, `shirou/gopsutil/v3/cpu` v3.23.12 is a `stub` (it returns `ErrNotImplementedError`) and so is `shoenig/go-m1cpu` v0.1.6 (it panics). `google/gopacket/pcap` v1.1.19 is `needs-cgo` on linux/amd64 and darwin/arm64, confirmed by `go build` failing on names declared in `pcap_unix.go`, and quiet on windows/amd64, where it doesn't use cgo. `prometheus/node_exporter` v1.8.2 is `needs-cgo` for darwin/arm64 and quiet for linux/amd64 and windows/amd64. A program using `modernc.org/sqlite` v1.34.4 and `net/http`, `junegunn/fzf` v0.56.3 and `mikefarah/yq` v4.44.6 stay quiet on all three targets. These need the network and aren't part of `make test`.
- **Input it can't trust:** tests put six kinds of bad content (empty, garbage, binary, failed build output, a panic trace, a comment without a package clause) in six files of `examples/photosvc`, one at a time with the others real, and check for exit 2 with nothing on stdout under every `--fail-on` and both formats. Others cut every file at every line break and in the middle of every line: each cut is refused or still reports the cut package, and on a machine with a C compiler a cut that breaks the cgo build is refused. Others cover type errors, missing imports and modules, broken go.mod files, packages with only test files, bad patterns and targets, and an empty module cache filled from a file-based `GOPROXY`, with and without `GOPROXY=off`.
- **Go versions:** the tests pass on Go 1.22.12 and Go 1.27.3 on Linux arm64, which also covers a host target with a C compiler and cross targets without one.

Not tested: running cgostub on macOS or Windows; binaries for macOS or Windows were built but not run; a fresh Claude Code session using the skill. The `go run ...@v0.1.0` line works only once the repository is published.

## Contributing

Contributions are welcome, from people and from people working with AI agents. See [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues are [listed here](https://github.com/GillMacedo/cgostub/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).

### How pull requests work

1. **Open a PR.** Fork the repo, work on a branch, and keep it to one change. Write a title that says what changes for users, because it becomes the changelog line: `feat:` goes under Added and `fix:` under Fixed.
2. **Automatic check.** Code changes need tests, and `make test` must pass. Changes over 30 lines need a linked open issue (`Closes #<number>`), and a PR can have at most 800 changed lines. If something is missing, a comment tells you what to fix.
3. **Review.** Claude reviews each PR automatically. Code PRs also get a security review and are tested in a sandbox. The review then merges the PR or asks for changes, and a small problem may become a follow-up issue instead. A PR with no new commit 14 days after a change request is closed, and you can reopen it. PRs that change CI, agent settings, npm or Python dependencies, vendored code or build files can't be merged this way, and a comment says so. The same goes for anything the security review doesn't clear.
4. **Release.** A merge that changes what users install becomes a new version within about two hours, with a GitHub Release and, if the project has one, its npm package. Docs and CI changes wait for the next release.

## AI disclosure

This project was largely generated with Claude Code and reviewed by its maintainer.

## About me

I'm Giulliano Macedo, and I build developer tools at GoTech Solutions. Follow along or get in touch:

- GitHub: [GillMacedo](https://github.com/GillMacedo)
- LinkedIn: [giulliano](https://www.linkedin.com/in/giulliano/)
- X: [@giumacedo](https://x.com/giumacedo)

If `cgostub` saved you some prompts, a star helps others find it.

---

Built by [Giulliano Macedo](https://github.com/GillMacedo), GoTech Solutions · [LinkedIn](https://www.linkedin.com/in/giulliano/) · [X](https://x.com/giumacedo)
