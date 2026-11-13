package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type jsonReport struct {
	Errors   int
	Warnings int
	Targets  []struct {
		Target          string
		Packages        int
		NonStandard     int `json:"packagesOutsideStandardLibrary"`
		BuiltWithCgo    bool
		BuiltWithoutCgo bool
		Findings        []struct {
			Severity, Code, Package, Message, Evidence string
			Via                                        []string
			Removed                                    []string `json:"filesOnlyWithCgo"`
			Added                                      []string `json:"filesOnlyWithoutCgo"`
		}
		Standard []struct{ Severity, Code, Package, Message string } `json:"standardLibrary"`
	}
}

func readJSON(t *testing.T, out string) jsonReport {
	t.Helper()
	var r jsonReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	return r
}

func codes(r jsonReport) map[string]string {
	m := map[string]string{}
	for _, tr := range r.Targets {
		for _, f := range tr.Findings {
			m[tr.Target+" "+f.Package] = f.Code
		}
	}
	return m
}

func TestPhotosvc(t *testing.T) {
	targets := cross() + "," + host()
	res := run(t, "../examples/photosvc", nil, "--target", targets, "--format", "json", "./...")
	if res.code != 1 || res.stderr != "" {
		t.Fatalf("exit %d, stderr %q", res.code, res.stderr)
	}
	r := readJSON(t, res.stdout)
	if len(r.Targets) != 3 || r.Errors != 3 || r.Warnings != 6 {
		t.Fatalf("got %d targets, %d errors, %d warnings:\n%s", len(r.Targets), r.Errors, r.Warnings, res.stdout)
	}
	got := codes(r)
	for _, tg := range strings.Split(targets, ",") {
		for pkg, code := range map[string]string{
			"example.com/photosvc/heic":     "stub",
			"example.com/photosvc/checksum": "fallback",
			"example.com/photosvc/thumbdb":  "cgo-dropped",
		} {
			if got[tg+" "+pkg] != code {
				t.Errorf("%s %s: got %q, want %q", tg, pkg, got[tg+" "+pkg], code)
			}
		}
	}
	for _, tr := range r.Targets {
		if tr.NonStandard != 4 || !tr.BuiltWithoutCgo {
			t.Errorf("%s: %d packages outside std, built without cgo %v", tr.Target, tr.NonStandard, tr.BuiltWithoutCgo)
		}
		if tr.BuiltWithCgo != (tr.Target == host() && hostCgo(t)) {
			t.Errorf("%s: built with cgo = %v", tr.Target, tr.BuiltWithCgo)
		}
		for _, f := range tr.Findings {
			if f.Package == "example.com/photosvc/heic" && !strings.Contains(f.Evidence, "decode_nocgo.go:7") {
				t.Errorf("stub evidence: %q", f.Evidence)
			}
			if f.Package == "example.com/photosvc/thumbdb" && !strings.Contains(f.Message, "init function in register_cgo.go:10") {
				t.Errorf("thumbdb message: %q", f.Message)
			}
			if len(f.Via) != 1 || f.Via[0] != "example.com/photosvc" {
				t.Errorf("%s via %v", f.Package, f.Via)
			}
		}
	}
}

func TestFailOn(t *testing.T) {
	tg := cross()
	cases := []struct {
		dir, pattern string
		want         map[string]int
	}{
		{"../examples/photosvc", "./...", map[string]int{"": 1, "error": 1, "warn": 1, "never": 0}},
		{"../examples/photosvc", "./checksum", map[string]int{"": 0, "error": 0, "warn": 1, "never": 0}},
		{"../examples/sensord", "./...", map[string]int{"": 1, "error": 1, "warn": 1, "never": 0}},
		{"../examples/healthy", ".", map[string]int{"": 0, "error": 0, "warn": 0, "never": 0}},
	}
	for _, c := range cases {
		for failOn, want := range c.want {
			args := []string{"--target", tg}
			if failOn != "" {
				args = append(args, "--fail-on", failOn)
			}
			for _, format := range []string{"text", "json", "markdown"} {
				res := run(t, c.dir, nil, append(args, "--format", format, c.pattern)...)
				if res.code != want {
					t.Errorf("%s %s --fail-on %q --format %s: exit %d, want %d\n%s%s", c.dir, c.pattern, failOn, format, res.code, want, res.stdout, res.stderr)
				}
			}
		}
	}
}

func TestHealthyStaysQuiet(t *testing.T) {
	for _, tg := range []string{cross(), host(), "linux/amd64"} {
		res := run(t, "../examples/healthy", nil, "--target", tg, ".")
		if res.code != 0 || res.stderr != "" {
			t.Fatalf("%s: exit %d\n%s%s", tg, res.code, res.stdout, res.stderr)
		}
		if strings.Contains(res.stdout, "ERROR") || strings.Contains(res.stdout, "WARN") {
			t.Errorf("%s: healthy program has findings:\n%s", tg, res.stdout)
		}
		if !strings.Contains(res.stdout, "nothing outside the standard library changes without cgo (1 package checked)") {
			t.Errorf("%s: summary missing:\n%s", tg, res.stdout)
		}
	}
}

func TestSensord(t *testing.T) {
	res := run(t, "../examples/sensord", nil, "--target", cross(), "--format", "json", "./...")
	r := readJSON(t, res.stdout)
	if res.code != 1 || r.Errors != 2 {
		t.Fatalf("exit %d, %d errors\n%s", res.code, r.Errors, res.stdout)
	}
	for _, tr := range r.Targets {
		if len(tr.Findings) != 1 || tr.Findings[0].Code != "needs-cgo" || tr.Findings[0].Package != "example.com/sensord/lmsensors" || tr.BuiltWithoutCgo {
			t.Errorf("%s: %+v", tr.Target, tr)
		}
	}
}

// TestMarkdown checks --format markdown: one table per target with the
// code, package, import chain and message, and the summary under it.
func TestMarkdown(t *testing.T) {
	targets := cross()
	res := run(t, "../examples/photosvc", nil, "--target", targets, "--format", "markdown", "./...")
	if res.code != 1 || res.stderr != "" {
		t.Fatalf("exit %d, stderr %q", res.code, res.stderr)
	}
	sections := strings.Split(res.stdout, "### ")
	if sections[0] != "" || len(sections) != 3 {
		t.Fatalf("want one section per target:\n%s", res.stdout)
	}
	for i, tg := range strings.Split(targets, ",") {
		sec := sections[i+1]
		if !strings.HasPrefix(sec, tg+"\n\n| Severity | Code | Package | Imported by | Message |\n| --- | --- | --- | --- | --- |\n") {
			t.Errorf("%s: section doesn't start with its table:\n%s", tg, sec)
		}
		for _, row := range []string{
			"| ERROR | stub | example.com/photosvc/heic | example.com/photosvc | ",
			"| WARN | fallback | example.com/photosvc/checksum | example.com/photosvc | ",
			"| WARN | cgo-dropped | example.com/photosvc/thumbdb | example.com/photosvc | ",
		} {
			if !strings.Contains(sec, "\n"+row) {
				t.Errorf("%s: missing row %q:\n%s", tg, row, sec)
			}
		}
		if !strings.Contains(sec, "|\n\n"+tg+": 1 error and 2 warnings in 4 packages outside the standard library.") {
			t.Errorf("%s: summary missing under the table:\n%s", tg, sec)
		}
	}

	res = run(t, "../examples/healthy", nil, "--target", cross(), "--format", "markdown", ".")
	if res.code != 0 || strings.Contains(res.stdout, "| Severity |") || !strings.Contains(res.stdout, "nothing outside the standard library changes without cgo (1 package checked)") {
		t.Errorf("healthy: exit %d\n%s", res.code, res.stdout)
	}
}

// TestRealBinaries builds photosvc with and without cgo, runs both, and
// checks that each finding matches what the binaries really do: the stub
// returns an error, the fallback gives the same checksum, and the driver
// registered from a cgo file is missing. It needs a C compiler.
func TestRealBinaries(t *testing.T) {
	if !hostCgo(t) {
		t.Skip("no C compiler: go env CGO_ENABLED is 0")
	}
	dir := t.TempDir()
	outputs := map[string]string{}
	for _, cgo := range []string{"1", "0"} {
		exe := filepath.Join(dir, "photosvc"+cgo)
		build := exec.Command("go", "build", "-o", exe, ".")
		build.Dir = "../examples/photosvc"
		build.Env = append(os.Environ(), "CGO_ENABLED="+cgo)
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("CGO_ENABLED=%s build: %v\n%s", cgo, err, out)
		}
		out, _ := exec.Command(exe).Output()
		outputs[cgo] = string(out)
	}
	lines := func(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") }
	with, without := lines(outputs["1"]), lines(outputs["0"])
	if len(with) != 3 || len(without) != 3 {
		t.Fatalf("unexpected output:\n%s\n%s", outputs["1"], outputs["0"])
	}
	if with[0] != "heic: 40 pixels, err=<nil>" || !strings.Contains(without[0], "decoding needs cgo") {
		t.Errorf("stub: %q vs %q", with[0], without[0])
	}
	if with[1] != without[1] {
		t.Errorf("fallback gives a different checksum: %q vs %q", with[1], without[1])
	}
	if with[2] != "drivers: [thumbs]" || without[2] != "drivers: []" {
		t.Errorf("driver: %q vs %q", with[2], without[2])
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "sensord"), ".")
	build.Dir = "../examples/sensord"
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if err := build.Run(); err == nil {
		t.Errorf("sensord built without cgo, but cgostub says it can't")
	}
}
