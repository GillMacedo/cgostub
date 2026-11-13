# 0002: Compare what go list selects, not build constraints

**Decision:** cgostub asks `go list` which files each package compiles with and without cgo, and compares the two lists. It doesn't evaluate `//go:build` lines itself.

**Why:** the go command's file selection includes file name suffixes, `cgo` as an implicit tag, `CgoFiles`, release tags and module and vendor rules. Reimplementing that would drift from the go command. Asking it is exact, works for any target without a C compiler, and covers the standard library the same way as modules.

**Cost:** two `go list -deps` runs per target. They are fast compared with the builds.
