package tests

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmptyModuleCache runs cgostub with an empty module cache and a
// file-based GOPROXY, so go list prints "go: downloading" lines on stderr
// while it fills the cache. Those lines must not make cgostub refuse, and the
// dependency's stub must still be found.
func TestEmptyModuleCache(t *testing.T) {
	proxy := t.TempDir()
	const mod, ver = "example.org/sqlish", "v1.0.0"
	base := filepath.Join(proxy, filepath.FromSlash(mod), "@v")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	gomod := "module " + mod + "\n\ngo 1.22\n"
	files := map[string]string{
		"go.mod":       gomod,
		"sqlish.go":    "// Package sqlish is a test module.\npackage sqlish\n\n// Open opens a database.\nfunc Open() error { return open() }\n",
		"open_cgo.go":  "package sqlish\n\n/*\nstatic int ok(void) { return 0; }\n*/\nimport \"C\"\n\nfunc open() error {\n\tC.ok()\n\treturn nil\n}\n",
		"open_stub.go": "//go:build !cgo\n\npackage sqlish\n\nimport \"errors\"\n\nfunc open() error { return errors.New(\"sqlish requires cgo\") }\n",
	}
	zf, err := os.Create(filepath.Join(base, ver+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for name, content := range files {
		w, err := zw.Create(mod + "@" + ver + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()
	writeFiles(t, base, map[string]string{
		"list":        ver + "\n",
		ver + ".mod":  gomod,
		ver + ".info": `{"Version":"` + ver + `","Time":"2026-01-01T00:00:00Z"}`,
	})

	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.22\n\nrequire " + mod + " " + ver + "\n",
		"main.go": "package main\n\nimport \"" + mod + "\"\n\nfunc main() { _ = sqlish.Open() }\n",
	})
	cache := filepath.Join(t.TempDir(), "modcache")
	env := []string{
		"GOPROXY=file://" + filepath.ToSlash(proxy),
		"GOMODCACHE=" + cache,
		"GOFLAGS=-mod=mod -modcacherw",
		"GOSUMDB=off",
		"GONOSUMDB=*",
		"GOPRIVATE=",
	}
	res := run(t, dir, env, "--target", cross(), "--format", "json", ".")
	if res.code != 1 {
		t.Fatalf("exit %d\n%s%s", res.code, res.stdout, res.stderr)
	}
	r := readJSON(t, res.stdout)
	for _, tr := range r.Targets {
		if len(tr.Findings) != 1 || tr.Findings[0].Code != "stub" || !strings.Contains(tr.Findings[0].Evidence, "sqlish requires cgo") {
			t.Errorf("%s: %+v", tr.Target, tr.Findings)
		}
	}
	if _, err := os.Stat(filepath.Join(cache, "cache", "download", filepath.FromSlash(mod))); err != nil {
		t.Errorf("the module was not downloaded into the empty cache: %v", err)
	}

	// The same with GOPROXY=off and the cache empty again: go list can't
	// get the module, so nothing can be checked.
	cache2 := filepath.Join(t.TempDir(), "modcache")
	wantRefused(t, "module not downloadable", dir, []string{"GOPROXY=off", "GOMODCACHE=" + cache2, "GOFLAGS=-mod=mod -modcacherw", "GOSUMDB=off"}, "--target", cross(), ".")
}
