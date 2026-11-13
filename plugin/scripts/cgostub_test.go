package main

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func newCache() *fileCache {
	return &fileCache{fset: token.NewFileSet(), files: map[string]*ast.File{}, endsWithNewline: map[string]bool{}}
}

func writeGo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestFindStub(t *testing.T) {
	cases := []struct {
		src, want string
	}{
		{"package p\n\nimport \"errors\"\n\nvar e = errors.New(\"p requires cgo\")\n", `a.go:5 has "p requires cgo"`},
		{"package p\n\nfunc F() { panic(\"no\") }\n", `a.go:3 calls panic("no")`},
		{"package p\n\nvar ErrNotImplemented error\n\nfunc F() error { return ErrNotImplemented }\n", "a.go:3 uses ErrNotImplemented"},
		{"package p\n\nfunc F() (int, error) { return 0, errUnsupported }\n\nvar errUnsupported error\n", "a.go:3 uses errUnsupported"},
		{"package p\n\nfunc F() string { return \"this feature is not implemented\" }\n", `a.go:3 has "this feature is not implemented"`},
		// A real implementation, with an import path that mentions cgo,
		// which must not count.
		{"package p\n\nimport _ \"example.com/cgo-helpers\"\n\nfunc Sum(b []byte) (n int) {\n\tfor _, c := range b {\n\t\tn += int(c)\n\t}\n\treturn n\n}\n", ""},
	}
	for _, c := range cases {
		dir := writeGo(t, map[string]string{"a.go": c.src})
		got, err := findStub(newCache(), dir, []string{"a.go"})
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%q: got %q, want %q", c.src, got, c.want)
		}
	}
}

func TestFindInitsAndDeclared(t *testing.T) {
	dir := writeGo(t, map[string]string{
		"a.go": "package p\n\nimport \"C\"\n\nconst limit = C.LIMIT\n\nvar x, _ = 1, 2\n\ntype T struct{}\n\nfunc (T) Method() {}\n\nfunc init() {}\n\nfunc helper() {}\n",
	})
	c := newCache()
	inits, err := findInits(c, dir, []string{"a.go"})
	if err != nil || !reflect.DeepEqual(inits, []string{"a.go:13"}) {
		t.Errorf("inits: %v %v", inits, err)
	}
	d, err := declared(c, dir, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"limit", "x", "T", "Method", "init", "helper"} {
		if d[name] != "a.go" {
			t.Errorf("%s not declared: %v", name, d)
		}
	}
	if _, ok := d["_"]; ok {
		t.Errorf("blank identifier counted")
	}
}

func TestFileDiff(t *testing.T) {
	on := &pkg{GoFiles: []string{"a.go", "tagged.go"}, CgoFiles: []string{"c.go"}}
	off := &pkg{GoFiles: []string{"a.go", "stub.go"}}
	removed, added := fileDiff(on, off)
	if !reflect.DeepEqual(removed, []string{"c.go", "tagged.go"}) || !reflect.DeepEqual(added, []string{"stub.go"}) {
		t.Errorf("removed %v, added %v", removed, added)
	}
	cgo, tagged := splitRemoved(on, removed)
	if !reflect.DeepEqual(cgo, []string{"c.go"}) || !reflect.DeepEqual(tagged, []string{"tagged.go"}) {
		t.Errorf("cgo %v, tagged %v", cgo, tagged)
	}
	if !changed(on, off) || changed(off, off) {
		t.Errorf("changed is wrong")
	}
}

func TestImportChains(t *testing.T) {
	pkgs := []*pkg{
		{ImportPath: "m", Name: "main", Imports: []string{"a", "b"}},
		{ImportPath: "a", Name: "a", Imports: []string{"c"}},
		{ImportPath: "b", Name: "b", Imports: []string{"c"}},
		{ImportPath: "c", Name: "c", DepOnly: true},
	}
	got := importChains(pkgs, index(pkgs))
	want := map[string][]string{"a": {"m"}, "b": {"m"}, "c": {"m", "a"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCgoCause(t *testing.T) {
	dir := writeGo(t, map[string]string{
		"p.go":     "package p\n\nfunc F() int { return cLimit }\n",
		"p_c.go":   "package p\n\nimport \"C\"\n\nconst cLimit = 1\n",
		"q_off.go": "package p\n",
	})
	on := &pkg{ImportPath: "p", Name: "p", Dir: dir, GoFiles: []string{"p.go"}, CgoFiles: []string{"p_c.go"}}
	off := &pkg{ImportPath: "p", Name: "p", Dir: dir, GoFiles: []string{"p.go", "q_off.go"}}
	onIdx, offIdx := index([]*pkg{on}), index([]*pkg{off})
	c := newCache()
	got, err := cgoCause(c, "p", []string{"p.go:3:23: undefined: cLimit"}, onIdx, offIdx)
	if err != nil || !strings.Contains(got, "cLimit is declared only in p_c.go") {
		t.Errorf("own: %q %v", got, err)
	}
	got, _ = cgoCause(c, "p", []string{"q_off.go:1:1: something"}, onIdx, offIdx)
	if !strings.Contains(got, "q_off.go, which is compiled only without cgo") {
		t.Errorf("added file: %q", got)
	}
	got, _ = cgoCause(c, "p", []string{"p.go:3:23: undefined: somethingElse"}, onIdx, offIdx)
	if got != "" {
		t.Errorf("unrelated error attributed to cgo: %q", got)
	}

	// A package that uses p.Limit, which p declares only in a cgo file.
	user := writeGo(t, map[string]string{"u.go": "package u\n\nimport \"p\"\n\nvar _ = p.Limit\n"})
	pdir := writeGo(t, map[string]string{"c.go": "package p\n\nimport \"C\"\n\nconst Limit = 1\n"})
	onP := &pkg{ImportPath: "p", Name: "p", Dir: pdir, CgoFiles: []string{"c.go"}}
	offP := &pkg{ImportPath: "p", Name: "p", Dir: pdir}
	u := &pkg{ImportPath: "u", Name: "u", Dir: user, GoFiles: []string{"u.go"}, Imports: []string{"p"}}
	got, _ = cgoCause(c, "u", []string{"u.go:5:11: undefined: p.Limit"}, index([]*pkg{onP, u}), index([]*pkg{offP, u}))
	if !strings.Contains(got, "p.Limit is declared only in c.go of p") {
		t.Errorf("import: %q", got)
	}
}

func TestRunFlags(t *testing.T) {
	var out, errb strings.Builder
	if code := run([]string{"--version"}, &out, &errb); code != 0 || out.String() != "cgostub "+version+"\n" {
		t.Errorf("--version: %d %q", code, out.String())
	}
	for _, args := range [][]string{{"--format", "yaml"}, {"--fail-on", "x"}, {"--target", "linux"}, {"--target", "a/b/c"}, {"--nope"}, {".", "-std"}} {
		out.Reset()
		if code := run(args, &out, &errb); code != 2 || out.Len() != 0 {
			t.Errorf("%v: exit %d, stdout %q", args, code, out.String())
		}
	}
}
