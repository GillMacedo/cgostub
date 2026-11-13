package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// untrusted is an error that means nothing could be checked. It always exits 2.
type untrusted struct{ msg string }

func (u *untrusted) Error() string { return u.msg }

func refuse(format string, args ...any) error {
	return &untrusted{fmt.Sprintf(format, args...)}
}

type listError struct {
	Pos         string
	Err         string
	ImportStack []string
}

// pkg holds the go list fields cgostub reads.
type pkg struct {
	ImportPath string
	Name       string
	Dir        string
	Standard   bool
	DepOnly    bool
	GoFiles    []string
	CgoFiles   []string
	Imports    []string
	Module     *struct {
		Path    string
		Version string
		Main    bool
	}
	Error      *listError
	DepsErrors []*listError
}

const listFields = "ImportPath,Name,Dir,Standard,DepOnly,GoFiles,CgoFiles,Imports,Module,Error,DepsErrors"

// config is one way of building the packages: a target and a cgo setting.
type config struct {
	goos, goarch string
	cgo          bool
	tags         string
}

func (c config) env() []string {
	env := removeEnv(os.Environ(), "CGO_ENABLED", "GOOS", "GOARCH")
	cgo := "0"
	if c.cgo {
		cgo = "1"
	}
	return append(env, "CGO_ENABLED="+cgo, "GOOS="+c.goos, "GOARCH="+c.goarch)
}

func (c config) String() string {
	cgo := "0"
	if c.cgo {
		cgo = "1"
	}
	return fmt.Sprintf("%s/%s with CGO_ENABLED=%s", c.goos, c.goarch, cgo)
}

// goList runs go list -deps for the patterns in one configuration and returns
// the packages in the order go list prints them.
func goList(dir string, c config, patterns []string) ([]*pkg, error) {
	args := []string{"list", "-e", "-deps", "-json=" + listFields}
	if c.tags != "" {
		args = append(args, "-tags", c.tags)
	}
	args = append(args, "--")
	args = append(args, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = c.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, refuse("go list for %s failed: %s", c, firstLines(stderr.String(), err.Error(), 6))
	}
	// go list prints "go: downloading MODULE VERSION" while it fills the
	// module cache. Anything else on stderr, such as "matched no packages",
	// means the result can't be trusted.
	var other []string
	for _, line := range strings.Split(stderr.String(), "\n") {
		if f := strings.Fields(line); len(f) == 0 || (len(f) == 4 && f[0] == "go:" && f[1] == "downloading") {
			continue
		}
		other = append(other, line)
	}
	if msg := strings.Join(other, "\n"); msg != "" {
		// go list -e prints "matched no packages" and similar warnings here.
		return nil, refuse("go list for %s printed: %s", c, firstLines(msg, "", 6))
	}
	var pkgs []*pkg
	dec := json.NewDecoder(&stdout)
	for {
		p := new(pkg)
		err := dec.Decode(p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, refuse("can't read go list output for %s: %v", c, err)
		}
		if p.ImportPath == "" {
			return nil, refuse("go list for %s printed a package without an import path", c)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		return nil, refuse("go list for %s found no packages", c)
	}
	roots := 0
	for _, p := range pkgs {
		if !p.DepOnly {
			roots++
		}
	}
	if roots == 0 {
		return nil, refuse("go list for %s found no packages matching %s", c, strings.Join(patterns, " "))
	}
	return pkgs, nil
}

// goEnv runs go env in the user's environment, with GOOS and GOARCH set
// when goos is not empty. CGO_ENABLED is left as the user has it, so the
// value reported is what a plain go build would use.
func goEnv(dir, goos, goarch string, names ...string) ([]string, error) {
	cmd := exec.Command("go", append([]string{"env"}, names...)...)
	cmd.Dir = dir
	env := os.Environ()
	if goos != "" {
		env = append(removeEnv(env, "GOOS", "GOARCH"), "GOOS="+goos, "GOARCH="+goarch)
	}
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, refuse("go env failed: %s", firstLines(stderr.String(), err.Error(), 4))
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(names) {
		return nil, refuse("go env printed %d lines for %d names", len(lines), len(names))
	}
	return lines, nil
}

func removeEnv(env []string, names ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		keep := true
		for _, n := range names {
			if k == n {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}

// buildResult is the outcome of a real build without cgo.
type buildResult struct {
	ok bool
	// failed maps an import path to the compiler's first lines for it.
	failed map[string][]string
	// other holds output lines that don't belong to a "# package" section.
	other []string
}

// goBuild builds the patterns without cgo and writes nothing outside tmp.
func goBuild(dir string, c config, patterns []string, anyMain bool, tmp string) (buildResult, error) {
	args := []string{"build"}
	if anyMain {
		args = append(args, "-o", tmp+string(filepath.Separator))
	}
	if c.tags != "" {
		args = append(args, "-tags", c.tags)
	}
	args = append(args, "--")
	args = append(args, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = c.env()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return buildResult{ok: true}, nil
	}
	if _, ok := err.(*exec.ExitError); !ok {
		return buildResult{}, refuse("go build for %s couldn't run: %v", c, err)
	}
	res := buildResult{failed: map[string][]string{}}
	current := ""
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.HasPrefix(line, "# ") {
			current = strings.TrimPrefix(line, "# ")
			// "# example.com/p [example.com/p.test]" appears for tests only.
			current, _, _ = strings.Cut(current, " ")
			if _, ok := res.failed[current]; !ok {
				res.failed[current] = nil
			}
			continue
		}
		if current == "" {
			if strings.TrimSpace(line) != "" {
				res.other = append(res.other, line)
			}
			continue
		}
		res.failed[current] = append(res.failed[current], strings.TrimSpace(line))
	}
	if len(res.failed) == 0 && len(res.other) == 0 {
		res.other = []string{err.Error()}
	}
	return res, nil
}

func (r buildResult) summary() string {
	var parts []string
	for _, p := range sortedKeys(r.failed) {
		parts = append(parts, p+": "+compilerLines(r.failed[p], ""))
	}
	parts = append(parts, r.other...)
	return firstLines(strings.Join(parts, "\n"), "go build failed", 6)
}

func firstLines(s, fallback string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = append(lines[:n], fmt.Sprintf("(%d more lines)", len(lines)-n))
	}
	return strings.Join(lines, "\n  ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
