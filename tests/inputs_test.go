package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each bad input must exit 2 with nothing on stdout, whatever --fail-on and
// --format say, and say that nothing was checked.
func wantRefused(t *testing.T, name, dir string, env []string, args ...string) {
	t.Helper()
	for _, failOn := range []string{"error", "warn", "never"} {
		for _, format := range []string{"text", "json", "markdown"} {
			a := append([]string{"--fail-on", failOn, "--format", format}, args...)
			res := run(t, dir, env, a...)
			if res.code != 2 || res.stdout != "" || !strings.HasPrefix(res.stderr, "cgostub: can't trust the input, nothing was checked: ") {
				t.Errorf("%s (--fail-on %s --format %s): exit %d\nstdout: %s\nstderr: %s", name, failOn, format, res.code, res.stdout, res.stderr)
				return
			}
		}
	}
}

const failedBuild = `# example.com/photosvc/heic
heic/heic.go:12:9: undefined: decode
`

const failedRun = `panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x47a2b6]

goroutine 1 [running]:
main.main()
	/src/main.go:9 +0x16
exit status 2
`

var badContents = map[string]string{
	"empty":                             "",
	"garbage":                           "this is not Go source at all {{{ ]]\n",
	"binary":                            "\x00\x01\x02\xff\xfe\x7fELF\x00\x00",
	"failed build":                      failedBuild,
	"failed run":                        failedRun,
	"only a comment, no package clause": "// Package heic decodes images.\n",
}

// TestBadFiles puts each kind of bad content in each kind of file of the
// photosvc example: a package's common file, a file that uses cgo, a file
// built only without cgo, and the main package. The other files stay real.
func TestBadFiles(t *testing.T) {
	files := []string{"main.go", "heic/heic.go", "heic/decode_cgo.go", "heic/decode_nocgo.go", "checksum/sum_purego.go", "thumbdb/register_cgo.go"}
	for _, file := range files {
		for kind, content := range badContents {
			dir := copyDir(t, "../examples/photosvc")
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(file)), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, tg := range []string{cross(), host()} {
				wantRefused(t, file+" "+kind+" "+tg, dir, nil, "--target", tg, "./...")
			}
		}
	}
}

// TestNoFinalNewline cuts the last byte of each file: the file still
// parses, but gofmt'd files always end with a line break.
func TestNoFinalNewline(t *testing.T) {
	for _, file := range []string{"main.go", "heic/decode_cgo.go", "heic/decode_nocgo.go", "checksum/sum_purego.go"} {
		dir := copyDir(t, "../examples/photosvc")
		p := filepath.Join(dir, filepath.FromSlash(file))
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data[:len(data)-1], 0o644); err != nil {
			t.Fatal(err)
		}
		wantRefused(t, file+" without its final line break", dir, nil, "--target", cross(), "./...")
	}
}

// TestCutFiles cuts every file of the photosvc example at every line break
// and in the middle of every line. A cut file must never give a clean
// result: either cgostub refuses, or it still reports the cut package,
// because every package in photosvc changes without cgo. When this machine
// can build with cgo, a cut that breaks the cgo build must be refused.
func TestCutFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	files := []string{"main.go", "heic/heic.go", "heic/decode_cgo.go", "heic/decode_nocgo.go", "checksum/checksum.go", "checksum/sum_cgo.go", "checksum/sum_purego.go", "thumbdb/thumbdb.go", "thumbdb/register_cgo.go"}
	targets := []string{cross()}
	if hostCgo(t) {
		targets = append(targets, host())
	}
	for _, file := range files {
		orig, err := os.ReadFile(filepath.Join("../examples/photosvc", filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		var cuts []int
		for i, b := range orig {
			if b == '\n' && i+1 < len(orig) {
				cuts = append(cuts, i+1)
			}
		}
		lineCuts := len(cuts)
		start := 0
		for _, end := range cuts[:lineCuts] {
			if end-start > 4 {
				cuts = append(cuts, start+(end-start)/2)
			}
			start = end
		}
		pkgPath := "example.com/photosvc"
		if d := filepath.Dir(filepath.FromSlash(file)); d != "." {
			pkgPath += "/" + filepath.ToSlash(d)
		}
		dir := copyDir(t, "../examples/photosvc")
		target := filepath.Join(dir, filepath.FromSlash(file))
		for _, n := range cuts {
			if err := os.WriteFile(target, orig[:n], 0o644); err != nil {
				t.Fatal(err)
			}
			for _, tg := range targets {
				res := run(t, dir, nil, "--target", tg, "--format", "json", "./...")
				switch res.code {
				case 2:
					if res.stdout != "" {
						t.Errorf("%s cut at %d, %s: exit 2 with output", file, n, tg)
					}
				case 1:
					r := readJSON(t, res.stdout)
					found := false
					for _, tr := range r.Targets {
						for _, f := range tr.Findings {
							if f.Package == pkgPath || (pkgPath == "example.com/photosvc" && f.Code == "needs-cgo") {
								found = true
							}
						}
					}
					// A cut on a line break can leave a valid, shorter file.
					// If the cut removed the only thing that made the file
					// cgo-specific, the package really doesn't change any
					// more, and no tool can tell the file was cut.
					cut := string(orig[:n])
					wasCgo := strings.Contains(string(orig), "import \"C\"") || strings.Contains(string(orig), "//go:build")
					stillCgo := strings.Contains(cut, "import \"C\"") || strings.Contains(cut, "//go:build")
					if !found && !(wasCgo && !stillCgo && strings.HasSuffix(cut, "\n")) {
						t.Errorf("%s cut at %d, %s: the cut package is not reported:\n%s", file, n, tg, res.stdout)
					}
				default:
					t.Errorf("%s cut at %d, %s: exit %d\n%s%s", file, n, tg, res.code, res.stdout, res.stderr)
				}
			}
		}
	}
}

// TestBrokenBuilds covers programs that parse but don't build.
func TestBrokenBuilds(t *testing.T) {
	cases := map[string]map[string]string{
		// A type error in a file built both ways: not about cgo.
		"type error in a common file": {"heic/heic.go": "package heic\n\ntype Image struct{ Pixels int }\n\nfunc Decode(data []byte) (Image, error) { return decode(data), nil }\n"},
		// main calls a function that exists in neither build.
		"undefined in main": {"main.go": "package main\n\nimport \"example.com/photosvc/heic\"\n\nfunc main() { heic.Missing() }\n"},
		// No main function at all.
		"no main function": {"main.go": "package main\n\nimport _ \"example.com/photosvc/heic\"\n"},
		// An import that doesn't exist.
		"missing import": {"main.go": "package main\n\nimport _ \"example.com/photosvc/nothere\"\n\nfunc main() {}\n"},
		// An import of a module that isn't in go.mod.
		"missing module": {"main.go": "package main\n\nimport _ \"github.com/example/notinmod\"\n\nfunc main() {}\n"},
		// A broken go.mod.
		"garbage go.mod": {"go.mod": "this is not a go.mod\n"},
		"empty go.mod":   {"go.mod": ""},
	}
	for name, files := range cases {
		dir := copyDir(t, "../examples/photosvc")
		writeFiles(t, dir, files)
		wantRefused(t, name, dir, nil, "--target", cross(), "./...")
	}

	// A package excluded by build constraints in both builds, named directly.
	dir := copyDir(t, "../examples/photosvc")
	writeFiles(t, dir, map[string]string{"main.go": "//go:build ignore\n\npackage main\n\nfunc main() {}\n"})
	wantRefused(t, "excluded both ways", dir, nil, "--target", cross(), ".")

	// A required module that isn't in the module cache, with no network.
	dir = copyDir(t, "../examples/photosvc")
	writeFiles(t, dir, map[string]string{
		"go.mod":         "module example.com/photosvc\n\ngo 1.22\n\nrequire example.org/notcached v1.0.0\n",
		"extra/extra.go": "package extra\n\nimport _ \"example.org/notcached\"\n",
	})
	wantRefused(t, "module not available", dir, []string{"GOPROXY=off", "GOFLAGS=-mod=mod"}, "--target", cross(), "./...")

	// A type error only in a file that uses cgo can only be seen by a build
	// with cgo, which cgostub runs when the target is this machine.
	if hostCgo(t) {
		dir = copyDir(t, "../examples/photosvc")
		writeFiles(t, dir, map[string]string{"heic/decode_cgo.go": "package heic\n\nimport \"C\"\n\nfunc decode(data []byte) (Image, error) { return Image{Pixels: \"x\"}, nil }\n"})
		wantRefused(t, "type error in a cgo file", dir, nil, "--target", host(), "./...")
	}
}

func TestBadInvocations(t *testing.T) {
	photosvc := "../examples/photosvc"
	wantRefused(t, "pattern that matches nothing", photosvc, nil, "--target", cross(), "./nothing/...")
	wantRefused(t, "directory that doesn't exist", photosvc, nil, "--target", cross(), "./nothing")
	wantRefused(t, "a log file as the package", photosvc, nil, "--target", cross(), "build.log")
	wantRefused(t, "unknown target", photosvc, nil, "--target", "plan10/vax", "./...")
	wantRefused(t, "outside a module", t.TempDir(), []string{"GO111MODULE=on"}, "--target", cross(), "./...")
	testsOnly := t.TempDir()
	writeFiles(t, testsOnly, map[string]string{
		"go.mod":              "module example.com/testsonly\n\ngo 1.22\n",
		"onlytest/x_test.go":  "package onlytest\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n",
		"emptydir/README.txt": "not Go\n",
	})
	wantRefused(t, "only test files", testsOnly, nil, "--target", cross(), "./...")
	wantRefused(t, "only test files, named", testsOnly, nil, "--target", cross(), "./onlytest")
	wantRefused(t, "a directory without Go files", testsOnly, nil, "--target", cross(), "./emptydir")
	wantRefused(t, "no go on PATH", photosvc, []string{"PATH=" + t.TempDir()}, "./...")

	for _, args := range [][]string{{"--format", "xml"}, {"--fail-on", "sometimes"}, {"--target", "linux"}, {".", "--std"}} {
		res := run(t, photosvc, nil, args...)
		if res.code != 2 || res.stdout != "" {
			t.Errorf("%v: exit %d\n%s", args, res.code, res.stdout)
		}
	}
}
