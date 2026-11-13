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
// changes at all on darwin without cgo depends on the Go version (1.22 leaves
// cgo_unix_cgo_darwin.go out, current releases build the same files both
// ways), so the note is only checked when there is one.
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
	for _, f := range readJSON(t, res.stdout).Targets[0].Standard {
		if strings.Contains(f.Message, "/etc/resolv.conf") || strings.Contains(f.Message, "/etc/passwd") {
			t.Errorf("%s: darwin note uses the Linux explanation: %q", f.Package, f.Message)
		}
		// The generic message names the cgo file and stops there. Darwin gets
		// an explanation of what happens to the resolver instead.
		if strings.Contains(f.Message, "with nothing in its place") {
			t.Errorf("%s: darwin still gets the generic message: %q", f.Package, f.Message)
		}
		if f.Package == "net" && !strings.Contains(f.Message, "resolver") {
			t.Errorf("net: darwin note doesn't say what happens to the resolver: %q", f.Message)
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
	if len(readJSON(t, plain.stdout).Targets[0].Standard) == 0 {
		t.Fatalf("without tags the standard library notes should be there:\n%s", plain.stdout)
	}
	tagged := run(t, dir, nil, "--target", "linux/amd64", "--std", "--tags", "netgo,osusergo", "--format", "json", ".")
	if tagged.code != 0 || tagged.stderr != "" {
		t.Fatalf("exit %d, stderr %q\n%s", tagged.code, tagged.stderr, tagged.stdout)
	}
	if got := readJSON(t, tagged.stdout).Targets[0].Standard; len(got) != 0 {
		t.Errorf("-tags netgo,osusergo still reports standard library changes: %+v", got)
	}
}
