# Changelog

All notable changes are documented here, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. This project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- The `net` and `os/user` notes say that `-tags netgo` / `-tags osusergo` make the cgo build use the pure Go code too, and darwin gets its own explanation instead of the Linux one: there both builds keep the cgo resolver, so only the compiled files differ.

## [0.1.0] - 2026-11-13

### Added
- Compares `go list -deps` for the named packages with `CGO_ENABLED=1` and `CGO_ENABLED=0`, for one or more `--target` GOOS/GOARCH pairs and `--tags`, and reports every package outside the standard library whose compiled files change.
- Findings: `stub` (the files compiled only without cgo mention cgo, say something is not implemented or supported, panic, or use a name such as `ErrNotImplemented`), `needs-cgo` (doesn't build without cgo), `fallback` (a different implementation) and `cgo-dropped` (cgo files left out, naming `init` functions that no longer run), each with the import chain from the program.
- A real `go build` without cgo confirms build failures, which count as `needs-cgo` only when they point at names declared in cgo-only files. When the target is the host and Go has a C compiler, a build with cgo checks that the program builds at all.
- Notes for standard library packages, with explanations for `net` and `os/user` on Linux and similar systems, and `plugin` reported as a stub.
- Exit 2 with no result for input it can't trust: files that don't parse in either build, files of the main module without a final line break, type errors unrelated to cgo, missing imports or modules, broken go.mod files, patterns that match nothing, unknown targets and a missing `go` command.
- Text and JSON output, `--fail-on error|warn|never`, `--std`, `--dir` and `--version`.
- `checking-go-builds-without-cgo` skill and plugin manifest.
