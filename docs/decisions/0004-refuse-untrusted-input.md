# 0004: Refuse input it can't trust, with exit 2

**Decision:** cgostub exits 2 and prints nothing on stdout when go list or go build fail for a reason that isn't cgo, when any file outside the standard library doesn't parse in either build, when a file of the main module has no final line break, when the patterns match nothing, and when the program doesn't build even with cgo. `--fail-on` doesn't change this.

**Why:** a check that says "nothing changes" for a program that doesn't build, or for a file cut in half, is worse than no check. go list reads only the start of each file, so a broken file that only one build compiles would otherwise pass unseen.

**Cost:** a file without a final line break is refused even when it is complete. The message says to run gofmt, which adds one.
