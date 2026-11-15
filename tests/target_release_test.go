package tests

import "testing"

// releaseTargets as cgostub defines them; the release shorthand is checked
// against these in tests/examples_test.go, which cannot import the package.
var releaseSet = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"}

// The end-to-end version of the same overlap: `release,linux/amd64` must not
// check linux/amd64 twice and double its findings.
func TestTargetReleaseWithAnOverlappingPair(t *testing.T) {
	res := run(t, "../examples/photosvc", nil, "--target", "release,linux/amd64", "--fail-on", "never",
		"--format", "json", "./...")
	r := readJSON(t, res.stdout)
	if len(r.Targets) != len(releaseSet) {
		t.Fatalf("got %d target reports for release,linux/amd64:\n%s", len(r.Targets), res.stdout)
	}
	seen := map[string]bool{}
	for _, tr := range r.Targets {
		if seen[tr.Target] {
			t.Fatalf("%s is reported twice", tr.Target)
		}
		seen[tr.Target] = true
	}
}
