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
