# Roadmap

## Next
- Read the targets and `CGO_ENABLED` settings from `.goreleaser.yaml` builds, so the release config is checked as written
- An allow file for findings a team has reviewed, such as a fallback known to behave the same
- A `--test` mode that also checks the packages `go test` builds

## Later
- Read `CGO_ENABLED`, `GOOS` and `GOARCH` from Dockerfile `RUN go build` lines and CI workflow matrices
- A GitHub Action that comments the findings on pull requests that add a dependency
- A list of known pure Go alternatives for common cgo packages, with the evidence behind each
