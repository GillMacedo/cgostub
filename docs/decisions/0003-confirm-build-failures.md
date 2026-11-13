# 0003: Confirm build failures, and blame cgo only when the errors point at it

**Decision:** cgostub runs a real `go build` without cgo. A compile error becomes `needs-cgo` only when it names something declared only in files that need cgo, or is in a file compiled only without cgo. Other compile errors are refused. When the target is the host and Go has a C compiler, it also builds with cgo and refuses a program that fails there.

**Why:** many packages that need cgo still list cleanly without it and only fail to compile, such as gopacket's pcap. A failure from an unrelated type error must not be reported as a cgo problem, because that hides the real one.

**Cost:** builds take time, especially the cgo build of large C code such as SQLite the first time. The build with cgo can't run for other targets without a cross compiler, so type errors in cgo-only files for those targets aren't seen. The README says so.
