// Package tests runs the cgostub binary against the example programs, real
// standard library packages and broken input.
package tests

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cgostub-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	bin = filepath.Join(dir, "cgostub")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, "../plugin/scripts").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "building cgostub: %v\n%s", err, out)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout, stderr string
	code           int
}

// run runs cgostub in dir with args and extra environment variables.
func run(t *testing.T, dir string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running cgostub: %v", err)
	}
	return result{stdout.String(), stderr.String(), code}
}

// host returns this machine's GOOS/GOARCH as go reports them.
func host() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// cross returns two targets that are not this machine, so the output never
// depends on whether a C compiler is installed here.
func cross() string {
	var out []string
	for _, t := range []string{"darwin/arm64", "windows/amd64", "linux/riscv64"} {
		if t != host() {
			out = append(out, t)
		}
	}
	return strings.Join(out[:2], ",")
}

// hostCgo reports whether go can build with cgo on this machine.
func hostCgo(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("go", "env", "CGO_ENABLED").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out)) == "1"
}

// copyDir copies a directory tree, so tests can change a copy.
func copyDir(t *testing.T, src string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), filepath.Base(src))
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// goVersionMinor returns the minor version of the go on PATH ("1.22" -> 22),
// or -1 when it can't be read. Tests that depend on a standard library's build
// constraints use it: the set of files a package compiles without cgo changed
// between Go versions.
//
// A release candidate ("go1.24rc1") and a development build ("devel go1.24-...")
// are read leniently, because the version they are closest to decides how the
// standard library is built. A caller that gets -1 must skip its version-gated
// check, not treat it as an old toolchain.
func goVersionMinor(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return -1
	}
	minor, ok := parseGoVersionMinor(strings.TrimSpace(string(out)))
	if !ok {
		return -1
	}
	return minor
}

// parseGoVersionMinor reads "go1.23.4", "go1.24rc1" and "devel go1.25-abcdef"
// the same way: the minor number after "go1.".
func parseGoVersionMinor(v string) (int, bool) {
	i := strings.Index(v, "go1.")
	if i < 0 {
		return 0, false
	}
	digits := v[i+len("go1."):]
	for j, r := range digits {
		if r < '0' || r > '9' {
			digits = digits[:j]
			break
		}
	}
	if digits == "" {
		return 0, false
	}
	minor, err := strconv.Atoi(digits)
	if err != nil {
		return 0, false
	}
	return minor, true
}
