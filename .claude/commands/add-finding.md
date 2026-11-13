Add a finding to cgostub for: $ARGUMENTS

1. Find a real module that has the pattern, and name it and its version in the PR.
2. Add the check to `checkTarget` or a helper in `plugin/scripts/check.go`, with a code in kebab case, a severity, and a message that says what changes in the build without cgo and what that does at run time.
3. Add a module to `examples/` or write one in a test in `tests/`, plus a module that must stay quiet. If it can be checked offline, build and run it with and without cgo to show the behavior, as `TestRealBinaries` does.
4. Add the code to the table in `README.md` and to the skill's list in `plugin/skills/checking-go-builds-without-cgo/SKILL.md`.
5. Run `make test` on Go 1.22 and the current release.
