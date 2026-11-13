# 0001: One Go program, standard library only

**Decision:** cgostub is a single `package main` in `plugin/scripts` with no dependencies, run by a Claude Code skill. It is not an MCP server.

**Why:** the people who need it already have Go. A skill that builds one program with `go build` works offline, installs in two commands, and needs no npm package or release binaries. An agent needs nothing from it that a command line doesn't give.

**Cost:** the installed plugin can't have a `go.mod`, so no third-party packages can be used without copying them in.
