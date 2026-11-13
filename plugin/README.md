# cgostub plugin

This plugin adds the `checking-go-builds-without-cgo` skill to Claude Code. The skill runs `scripts/cgostub.go`, which compares `go list` for a Go program with `CGO_ENABLED=1` and `CGO_ENABLED=0` for each target, confirms with a real `go build` without cgo, and reports dependencies that compile a stub, switch to a different implementation, leave out cgo code, or don't build. It needs Go 1.22 or newer and the program's source with its dependencies. It doesn't change any files.

See the repository README for examples, exit codes and limits: https://github.com/GillMacedo/cgostub

## Install

    claude plugin marketplace add GillMacedo/cgostub
    claude plugin install cgostub@cgostub-marketplace
