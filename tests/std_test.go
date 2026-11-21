package tests

import (
	"strings"
	"testing"
)

// TestStandardLibrary checks real standard library packages: net and os/user
// have pure Go versions on Linux, and plugin is a stub without cgo.
func TestStandardLibrary(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/stdprog\n\ngo 1.22\n",
		"main.go": `package main

import (
	"fmt"
	"net"
	"os/user"
	"plugin"
)

func main() {
	fmt.Println(net.LookupHost("localhost"))
	fmt.Println(user.Current())
	fmt.Println(plugin.Open("x.so"))
}
`,
	})
	res := run(t, dir, nil, "--target", "linux/amd64", "--format", "json", ".")
	if res.code != 1 {
		t.Fatalf("exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	r := readJSON(t, res.stdout)
	tr := r.Targets[0]
	if len(tr.Findings) != 1 || tr.Findings[0].Package != "plugin" || tr.Findings[0].Code != "stub" {
		t.Errorf("findings: %+v", tr.Findings)
	}
	std := map[string]string{}
	for _, f := range tr.Standard {
		std[f.Package] = f.Message
		if f.Severity != "note" {
			t.Errorf("%s: severity %s", f.Package, f.Severity)
		}
	}
	if !strings.Contains(std["net"], "pure Go resolver") || !strings.Contains(std["os/user"], "/etc/passwd") {
		t.Errorf("standard library notes: %v", std)
	}

	// Without plugin, only notes remain, which never fail the run, and the
	// text output names them without listing them.
	writeFiles(t, dir, map[string]string{"main.go": `package main

import (
	"fmt"
	"net"
	"os/user"
)

func main() {
	fmt.Println(net.LookupHost("localhost"))
	fmt.Println(user.Current())
}
`})
	for _, failOn := range []string{"error", "warn"} {
		res = run(t, dir, nil, "--target", "linux/amd64", "--fail-on", failOn, ".")
		if res.code != 0 || !strings.Contains(res.stdout, "Standard library packages that also change: net, os/user (--std explains).") || strings.Contains(res.stdout, "note ") {
			t.Errorf("--fail-on %s: exit %d\n%s%s", failOn, res.code, res.stdout, res.stderr)
		}
	}
	res = run(t, dir, nil, "--target", "linux/amd64", "--std", ".")
	if res.code != 0 || !strings.Contains(res.stdout, "note standard-library: os/user, for linux/amd64") {
		t.Errorf("--std: exit %d\n%s", res.code, res.stdout)
	}
}

// TestStandardLibraryDarwin checks that darwin gets its own explanation, not
// the Linux one: darwin links the cgo resolver into every build, so the note
// must not claim that name lookups fall back to /etc/resolv.conf. Whether net
// changes at all on darwin without cgo depends on the Go version: 1.22 has
// cgo_unix_cgo_darwin.go, built with "!netgo && cgo && darwin", and 1.23
// removed that file, so later releases build the same net files both ways.
func TestStandardLibraryDarwin(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/stddarwin\n\ngo 1.22\n",
		"main.go": `package main

import (
	"fmt"
	"net"
	"os/user"
)

func main() {
	fmt.Println(net.LookupHost("localhost"))
	fmt.Println(user.Current())
}
`,
	})
	res := run(t, dir, nil, "--target", "darwin/arm64", "--std", "--format", "json", ".")
	if res.code != 0 {
		t.Fatalf("exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	notes := firstTarget(t, res.stdout).Standard
	// The darwin text itself is covered by TestStdExplainDarwin, which runs on
	// any toolchain. Here the note is checked only where the toolchain produces
	// one, so this test can never pass by looking at an empty list.
	if minor := goVersionMinor(t); minor < 0 {
		t.Logf("the go on PATH isn't a version this test can parse, so the net note can't be expected")
	} else if minor <= 22 {
		var network *struct{ Severity, Code, Package, Message string }
		for i := range notes {
			if notes[i].Package == "net" {
				network = &notes[i]
			}
		}
		if network == nil {
			t.Fatalf("Go 1.22 leaves a net file out on darwin, so there must be a net note: %s", res.stdout)
		}
		if !strings.Contains(network.Message, "resolver") {
			t.Errorf("net: darwin note doesn't say what happens to the resolver: %q", network.Message)
		}
		if !strings.Contains(network.Message, "cgo_unix_cgo_darwin.go") {
			t.Errorf("net: darwin note doesn't name the file that changed: %q", network.Message)
		}
	} else {
		t.Skipf("Go 1.%d compiles the same net files on darwin with and without cgo, so there is no note to check here", minor)
	}
	for _, f := range notes {
		if strings.Contains(f.Message, "/etc/resolv.conf") || strings.Contains(f.Message, "/etc/passwd") {
			t.Errorf("%s: darwin note uses the Linux explanation: %q", f.Package, f.Message)
		}
		// The generic message names the cgo file and stops there. Darwin gets
		// an explanation of what happens to the resolver instead.
		if strings.Contains(f.Message, "with nothing in its place") {
			t.Errorf("%s: darwin still gets the generic message: %q", f.Package, f.Message)
		}
	}
}

// TestStandardLibraryBuildTags checks the other side of the same coin: with
// -tags netgo,osusergo the cgo build uses the pure Go code too, so the two
// builds agree and cgostub has nothing to report.
func TestStandardLibraryBuildTags(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod": "module example.com/stdtags\n\ngo 1.22\n",
		"main.go": `package main

import (
	"fmt"
	"net"
	"os/user"
)

func main() {
	fmt.Println(net.LookupHost("localhost"))
	fmt.Println(user.Current())
}
`,
	})
	plain := run(t, dir, nil, "--target", "linux/amd64", "--std", "--format", "json", ".")
	if plain.code != 0 || plain.stderr != "" {
		t.Fatalf("exit %d, stderr %q\n%s", plain.code, plain.stderr, plain.stdout)
	}
	if firstTarget(t, plain.stdout).Standard == nil {
		t.Fatalf("without tags the standard library notes should be there:\n%s", plain.stdout)
	}
	tagged := run(t, dir, nil, "--target", "linux/amd64", "--std", "--tags", "netgo,osusergo", "--format", "json", ".")
	if tagged.code != 0 || tagged.stderr != "" {
		t.Fatalf("exit %d, stderr %q\n%s", tagged.code, tagged.stderr, tagged.stdout)
	}
	if got := firstTarget(t, tagged.stdout).Standard; len(got) != 0 {
		t.Errorf("-tags netgo,osusergo still reports standard library changes: %+v", got)
	}
}

// TestGoVersionMinor checks the lenient parsing the darwin test gates on: a
// release candidate and a development build must give their version, not -1,
// so a toolchain that reports one of those isn't taken for Go 1.22.
func TestGoVersionMinor(t *testing.T) {
	cases := map[string]int{
		"go1.22":              22,
		"go1.22.12":           22,
		"go1.23rc1":           23,
		"go1.24.0":            24,
		"devel go1.25-abcdef": 25,
		"devel +1234abcdef":   -1, // no version at all
		"gccgo":               -1,
		"don't know":          -1,
	}
	for v, want := range cases {
		got, ok := parseGoVersionMinor(v)
		if want == -1 {
			if ok {
				t.Errorf("%q: read as %d, want no version", v, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("%q: got %d (%v), want %d", v, got, ok, want)
		}
	}
}

// firstTarget parses a JSON report and returns its one target, failing the
// test when the report doesn't have exactly one, so a run that checked
// nothing can't be read as a quiet one.
func firstTarget(t *testing.T, out string) jsonTarget {
	t.Helper()
	r := readJSON(t, out)
	if len(r.Targets) != 1 {
		t.Fatalf("got %d targets, want 1:\n%s", len(r.Targets), out)
	}
	return r.Targets[0]
}
