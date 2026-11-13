package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type finding struct {
	Severity string   `json:"severity"`
	Code     string   `json:"code"`
	Package  string   `json:"package"`
	Standard bool     `json:"standard"`
	Via      []string `json:"via,omitempty"`
	Message  string   `json:"message"`
	Fix      string   `json:"fix,omitempty"`
	Removed  []string `json:"filesOnlyWithCgo,omitempty"`
	Added    []string `json:"filesOnlyWithoutCgo,omitempty"`
	Evidence string   `json:"evidence,omitempty"`
}

type targetReport struct {
	Target          string    `json:"target"`
	DefaultCgo      string    `json:"defaultCgoEnabled"`
	Packages        int       `json:"packages"`
	NonStandard     int       `json:"packagesOutsideStandardLibrary"`
	BuiltWithCgo    bool      `json:"builtWithCgo"`
	BuiltWithoutCgo bool      `json:"builtWithoutCgo"`
	Findings        []finding `json:"findings"`
	Standard        []finding `json:"standardLibrary"`
	host            string
}

const (
	sevError = "error"
	sevWarn  = "warn"
	sevNote  = "note"
)

// checkTarget lists the packages with and without cgo for one target,
// confirms the build without cgo, and reports every package that changes.
func checkTarget(dir, goos, goarch, tags string, patterns []string, tmp string, cgoBuild bool) (*targetReport, error) {
	target := goos + "/" + goarch
	on := config{goos: goos, goarch: goarch, cgo: true, tags: tags}
	off := on
	off.cgo = false

	onPkgs, err := goList(dir, on, patterns)
	if err != nil {
		return nil, err
	}
	offPkgs, err := goList(dir, off, patterns)
	if err != nil {
		return nil, err
	}
	onIdx, offIdx := index(onPkgs), index(offPkgs)

	for _, p := range onPkgs {
		if p.Error != nil {
			return nil, refuse("%s: %s doesn't load for %s: %s", target, p.ImportPath, on, listErr(p.Error))
		}
		if len(p.DepsErrors) > 0 {
			return nil, refuse("%s: %s has a dependency that doesn't load for %s: %s", target, p.ImportPath, on, listErr(p.DepsErrors[0]))
		}
	}

	withFiles := 0
	for _, p := range onPkgs {
		if !p.DepOnly && len(p.GoFiles)+len(p.CgoFiles) > 0 {
			withFiles++
		}
	}
	if withFiles == 0 {
		return nil, refuse("%s: none of the packages matching %s has Go files to build, other than tests", target, strings.Join(patterns, " "))
	}

	files := &fileCache{fset: token.NewFileSet(), files: map[string]*ast.File{}, endsWithNewline: map[string]bool{}}
	for _, list := range [][]*pkg{onPkgs, offPkgs} {
		for _, p := range list {
			if p.Standard {
				continue
			}
			for _, f := range append(append([]string{}, p.GoFiles...), p.CgoFiles...) {
				if _, err := files.parse(filepath.Join(p.Dir, f)); err != nil {
					return nil, refuse("%s: can't parse %s in %s: %v", target, f, p.ImportPath, err)
				}
				// gofmt ends every file with a line break, so a file of
				// the user's own module without one may have been cut off.
				if p.Module != nil && p.Module.Main && !files.endsWithNewline[filepath.Join(p.Dir, f)] {
					return nil, refuse("%s: %s in %s doesn't end with a line break, so it may be cut off; run gofmt on it if it is complete", target, f, p.ImportPath)
				}
			}
		}
	}

	needs := map[string]string{}
	for _, p := range offPkgs {
		if p.Error == nil {
			continue
		}
		o := onIdx[p.ImportPath]
		// Go 1.22 says "no Go files in DIR" here, later releases say
		// "build constraints exclude all Go files in DIR".
		noFiles := strings.Contains(p.Error.Err, "build constraints exclude all Go files") || strings.Contains(p.Error.Err, "no Go files in")
		if o != nil && noFiles && len(o.CgoFiles) > 0 && changed(o, p) {
			needs[p.ImportPath] = "build constraints exclude all of its Go files"
			continue
		}
		return nil, refuse("%s: %s doesn't load for %s: %s", target, p.ImportPath, off, listErr(p.Error))
	}
	for _, p := range offPkgs {
		for _, e := range p.DepsErrors {
			if len(e.ImportStack) > 0 {
				if _, ok := needs[e.ImportStack[len(e.ImportStack)-1]]; ok {
					continue
				}
			}
			return nil, refuse("%s: %s has a dependency that doesn't load for %s: %s", target, p.ImportPath, off, listErr(e))
		}
	}

	rep := &targetReport{Target: target, Packages: len(onPkgs)}
	for _, p := range onPkgs {
		if !p.Standard {
			rep.NonStandard++
		}
	}
	anyMain := false
	for _, p := range offPkgs {
		if !p.DepOnly && p.Name == "main" {
			anyMain = true
		}
	}
	if cgoBuild {
		res, err := goBuild(dir, on, patterns, anyMain, tmp)
		if err != nil {
			return nil, err
		}
		if !res.ok {
			return nil, refuse("%s: the packages don't build even with cgo, so there is nothing to compare: %s", target, res.summary())
		}
		rep.BuiltWithCgo = true
	}
	if len(needs) == 0 {
		res, err := goBuild(dir, off, patterns, anyMain, tmp)
		if err != nil {
			return nil, err
		}
		rep.BuiltWithoutCgo = true
		if !res.ok {
			if len(res.other) > 0 {
				return nil, refuse("%s: go build for %s failed: %s", target, off, firstLines(strings.Join(res.other, "\n"), "", 6))
			}
			for _, path := range sortedKeys(res.failed) {
				p := offIdx[path]
				if p == nil || onIdx[path] == nil {
					return nil, refuse("%s: go build for %s failed in %s, which go list didn't report: %s", target, off, path, compilerLines(res.failed[path], ""))
				}
				msg := compilerLines(res.failed[path], p.Dir)
				cause, err := cgoCause(files, path, res.failed[path], onIdx, offIdx)
				if err != nil {
					return nil, refuse("%s: %v", target, err)
				}
				if cause == "" {
					return nil, refuse("%s: %s doesn't compile for %s, and the errors don't point at code that changes with cgo, so the build is broken for another reason: %s", target, path, off, msg)
				}
				needs[path] = cause + ". go build says " + msg
			}
		}
	}

	via := importChains(onPkgs, onIdx)
	paths := make([]string, 0, len(onPkgs))
	for _, p := range onPkgs {
		paths = append(paths, p.ImportPath)
	}
	sort.Strings(paths)
	for _, path := range paths {
		o := onIdx[path]
		p := offIdx[path]
		if path == "runtime/cgo" || p == nil {
			// runtime/cgo exists only with cgo, and packages missing from
			// the build without cgo were imported only by cgo-only files of
			// a package reported below.
			continue
		}
		removed, added := fileDiff(o, p)
		reason, needsCgo := needs[path]
		if !needsCgo && len(removed) == 0 && len(added) == 0 {
			continue
		}
		f := finding{Package: path, Standard: o.Standard, Via: via[path], Removed: removed, Added: added}
		cgoFiles, cgoTagged := splitRemoved(o, removed)
		fix := fmt.Sprintf("build %s with CGO_ENABLED=1 and a C compiler for it, or use a package that doesn't need cgo", target)
		switch {
		case needsCgo:
			f.Severity, f.Code, f.Fix = sevError, "needs-cgo", fix
			f.Message = "it doesn't build without cgo: " + reason
		case len(added) > 0:
			ev, err := findStub(files, o.Dir, added)
			if err != nil {
				return nil, refuse("%s: %v", target, err)
			}
			what := "without cgo it compiles " + fileList(added) + " in place of " + describeRemoved(cgoFiles, cgoTagged)
			if ev != "" {
				f.Severity, f.Code, f.Fix, f.Evidence = sevError, "stub", fix, ev
				f.Message = what + ". That looks like a stub: " + ev
			} else {
				f.Severity, f.Code = sevWarn, "fallback"
				f.Message = what + ". It is a different implementation, so check that it behaves the same before you ship a build without cgo"
			}
		default:
			inits, err := findInits(files, o.Dir, removed)
			if err != nil {
				return nil, refuse("%s: %v", target, err)
			}
			f.Severity, f.Code = sevWarn, "cgo-dropped"
			place := "their place"
			if len(removed) == 1 {
				place = "its place"
			}
			f.Message = "without cgo it leaves out " + describeRemoved(cgoFiles, cgoTagged) + ", with nothing in " + place
			if len(inits) > 0 {
				f.Message += ", so the init function in " + strings.Join(inits, ", ") + " never runs"
			}
		}
		if o.Standard {
			f.Severity, f.Code, f.Fix, f.Evidence = sevNote, "standard-library", "", ""
			stdExplain(&f, goos)
			if f.Severity != sevError {
				rep.Standard = append(rep.Standard, f)
				continue
			}
		}
		rep.Findings = append(rep.Findings, f)
	}
	return rep, nil
}

var (
	undefinedRe = regexp.MustCompile(`undefined: ([A-Za-z_]\w*)(?:\.([A-Za-z_]\w*))?`)
	noMethodRe  = regexp.MustCompile(`has no field or method ([A-Za-z_]\w*)`)
)

// cgoCause explains why a package doesn't compile without cgo: a name it
// uses is declared only in files that need cgo, in the package itself or in
// a package it imports, or the error is in a file compiled only without cgo.
// It returns "" when the errors don't point at cgo.
func cgoCause(files *fileCache, path string, lines []string, onIdx, offIdx map[string]*pkg) (string, error) {
	p := offIdx[path]
	removed, added := fileDiff(onIdx[path], p)
	own, err := declared(files, p.Dir, removed)
	if err != nil {
		return "", err
	}
	type dep struct {
		path  string
		name  string
		decls map[string]string
	}
	var deps []dep
	for _, imp := range p.Imports {
		io, ip := onIdx[imp], offIdx[imp]
		if io == nil || ip == nil {
			continue
		}
		r, _ := fileDiff(io, ip)
		d, err := declared(files, io.Dir, r)
		if err != nil {
			return "", err
		}
		if len(d) > 0 {
			deps = append(deps, dep{imp, ip.Name, d})
		}
	}
	for _, line := range lines {
		for _, f := range added {
			if strings.Contains(line, string(filepath.Separator)+f+":") || strings.HasPrefix(line, f+":") {
				return "the error is in " + f + ", which is compiled only without cgo", nil
			}
		}
		if m := undefinedRe.FindStringSubmatch(line); m != nil {
			if m[2] == "" {
				if file, ok := own[m[1]]; ok {
					return m[1] + " is declared only in " + file + ", which needs cgo", nil
				}
			}
			for _, d := range deps {
				if m[2] != "" && d.name == m[1] {
					if file, ok := d.decls[m[2]]; ok {
						return fmt.Sprintf("%s.%s is declared only in %s of %s, which needs cgo", m[1], m[2], file, d.path), nil
					}
				}
			}
		}
		if m := noMethodRe.FindStringSubmatch(line); m != nil {
			if file, ok := own[m[1]]; ok {
				return m[1] + " is declared only in " + file + ", which needs cgo", nil
			}
			for _, d := range deps {
				if file, ok := d.decls[m[1]]; ok {
					return fmt.Sprintf("%s is declared only in %s of %s, which needs cgo", m[1], file, d.path), nil
				}
			}
		}
	}
	return "", nil
}

// declared returns the top-level names and method names declared in files.
func declared(c *fileCache, dir string, files []string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range files {
		f, err := c.parse(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("can't parse %s: %v", name, err)
		}
		add := func(id *ast.Ident) {
			if id != nil && id.Name != "_" {
				if _, ok := out[id.Name]; !ok {
					out[id.Name] = name
				}
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				add(d.Name)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						add(spec.Name)
					case *ast.ValueSpec:
						for _, n := range spec.Names {
							add(n)
						}
					}
				}
			}
		}
	}
	return out, nil
}

// compilerLines makes the compiler's paths relative to the package
// directory and keeps the first two lines.
func compilerLines(lines []string, dir string) string {
	var out []string
	for _, l := range lines {
		if len(out) == 2 {
			break
		}
		if dir != "" {
			l = strings.TrimPrefix(l, dir+string(filepath.Separator))
		}
		out = append(out, l)
	}
	return strings.Join(out, "; ")
}

func listErr(e *listError) string {
	msg := strings.TrimSpace(e.Err)
	if e.Pos != "" && !strings.Contains(msg, e.Pos) {
		msg = e.Pos + ": " + msg
	}
	return firstLines(msg, "unknown error", 4)
}

func index(pkgs []*pkg) map[string]*pkg {
	m := make(map[string]*pkg, len(pkgs))
	for _, p := range pkgs {
		m[p.ImportPath] = p
	}
	return m
}

func onFiles(p *pkg) []string {
	return append(append([]string{}, p.GoFiles...), p.CgoFiles...)
}

func fileDiff(on, off *pkg) (removed, added []string) {
	offSet := map[string]bool{}
	for _, f := range onFiles(off) {
		offSet[f] = true
	}
	onSet := map[string]bool{}
	for _, f := range onFiles(on) {
		onSet[f] = true
		if !offSet[f] {
			removed = append(removed, f)
		}
	}
	for _, f := range onFiles(off) {
		if !onSet[f] {
			added = append(added, f)
		}
	}
	sort.Strings(removed)
	sort.Strings(added)
	return removed, added
}

func changed(on, off *pkg) bool {
	r, a := fileDiff(on, off)
	return len(r) > 0 || len(a) > 0 || (len(on.CgoFiles) > 0 && off.Error != nil)
}

func splitRemoved(on *pkg, removed []string) (cgo, tagged []string) {
	isCgo := map[string]bool{}
	for _, f := range on.CgoFiles {
		isCgo[f] = true
	}
	for _, f := range removed {
		if isCgo[f] {
			cgo = append(cgo, f)
		} else {
			tagged = append(tagged, f)
		}
	}
	return cgo, tagged
}

func describeRemoved(cgo, tagged []string) string {
	var parts []string
	if len(cgo) > 0 {
		parts = append(parts, plural(len(cgo), "file that uses cgo", "files that use cgo")+" ("+fileList(cgo)+")")
	}
	if len(tagged) > 0 {
		parts = append(parts, plural(len(tagged), "file built only with cgo", "files built only with cgo")+" ("+fileList(tagged)+")")
	}
	if len(parts) == 0 {
		return "nothing"
	}
	return strings.Join(parts, " and ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func fileList(files []string) string {
	if len(files) > 4 {
		return strings.Join(files[:3], ", ") + fmt.Sprintf(" and %d more", len(files)-3)
	}
	return strings.Join(files, ", ")
}

// importChains returns, for each package, the shortest import path from the
// main packages the user named, or from all named packages if none is main.
func importChains(pkgs []*pkg, idx map[string]*pkg) map[string][]string {
	chains := map[string][]string{}
	var queue []string
	for _, p := range pkgs {
		if !p.DepOnly && p.Name == "main" {
			queue = append(queue, p.ImportPath)
		}
	}
	if len(queue) == 0 {
		for _, p := range pkgs {
			if !p.DepOnly {
				queue = append(queue, p.ImportPath)
			}
		}
	}
	sort.Strings(queue)
	for _, q := range queue {
		chains[q] = []string{q}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		imps := append([]string{}, idx[cur].Imports...)
		sort.Strings(imps)
		for _, imp := range imps {
			if _, seen := chains[imp]; seen || idx[imp] == nil {
				continue
			}
			chains[imp] = append(append([]string{}, chains[cur]...), imp)
			queue = append(queue, imp)
		}
	}
	out := map[string][]string{}
	for k, v := range chains {
		if len(v) > 1 {
			out[k] = v[:len(v)-1]
		}
	}
	return out
}

type fileCache struct {
	fset            *token.FileSet
	files           map[string]*ast.File
	endsWithNewline map[string]bool
}

func (c *fileCache) parse(path string) (*ast.File, error) {
	if f, ok := c.files[path]; ok {
		return f, nil
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := parser.ParseFile(c.fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	c.files[path] = f
	c.endsWithNewline[path] = len(src) > 0 && src[len(src)-1] == '\n'
	return f, nil
}

var (
	stubText  = regexp.MustCompile(`(?i)cgo|not (yet )?implemented|not supported|unsupported|\bstub\b`)
	stubIdent = regexp.MustCompile(`(?i)not_?implemented|not_?supported|unsupported`)
)

// findStub looks for signs that files compiled only without cgo are a stub:
// a panic call, a string that mentions cgo or says something is not
// implemented or supported, or an identifier such as ErrNotImplemented.
func findStub(c *fileCache, dir string, files []string) (string, error) {
	for _, name := range files {
		f, err := c.parse(filepath.Join(dir, name))
		if err != nil {
			return "", fmt.Errorf("can't parse %s: %v", name, err)
		}
		found := ""
		ast.Inspect(f, func(n ast.Node) bool {
			if found != "" {
				return false
			}
			switch n := n.(type) {
			case *ast.ImportSpec:
				return false
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "panic" {
					found = fmt.Sprintf("%s:%d calls panic", name, c.fset.Position(n.Pos()).Line)
					if len(n.Args) == 1 {
						if lit, ok := n.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							found += "(" + lit.Value + ")"
						}
					}
					return false
				}
			case *ast.BasicLit:
				if n.Kind == token.STRING {
					if s, err := strconv.Unquote(n.Value); err == nil && stubText.MatchString(s) {
						found = fmt.Sprintf("%s:%d has %s", name, c.fset.Position(n.Pos()).Line, n.Value)
					}
				}
			case *ast.Ident:
				if stubIdent.MatchString(n.Name) {
					found = fmt.Sprintf("%s:%d uses %s", name, c.fset.Position(n.Pos()).Line, n.Name)
				}
			}
			return true
		})
		if found != "" {
			return found, nil
		}
	}
	return "", nil
}

func findInits(c *fileCache, dir string, files []string) ([]string, error) {
	var out []string
	for _, name := range files {
		f, err := c.parse(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("can't parse %s: %v", name, err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "init" {
				out = append(out, fmt.Sprintf("%s:%d", name, c.fset.Position(fd.Pos()).Line))
			}
		}
	}
	return out, nil
}

// stdExplain replaces the generic message for standard library packages
// whose behavior without cgo is documented.
func stdExplain(f *finding, goos string) {
	unixLike := goos != "windows" && goos != "darwin" && goos != "ios" && goos != "plan9" && goos != "js" && goos != "wasip1"
	darwinLike := goos == "darwin" || goos == "ios"
	switch {
	case f.Package == "net" && unixLike:
		f.Message = "net can't use the C library's resolver, so name lookups always use the pure Go resolver, which reads /etc/hosts and queries the DNS servers in /etc/resolv.conf. Other NSS sources in /etc/nsswitch.conf, such as LDAP or mDNS, are not used"
		f.Message += ". Building with -tags netgo makes net use the pure Go resolver in the cgo build too, so both builds resolve names the same way"
	case f.Package == "net" && darwinLike:
		f.Message = "darwin always links the cgo resolver, so net keeps it even without cgo; what it leaves out is the file that talks to it, which does not change how names are resolved here"
	case f.Package == "os/user" && unixLike:
		f.Message = "os/user uses its pure Go implementation, which reads /etc/passwd and /etc/group only, so users and groups from LDAP, SSSD or other NSS sources are not found"
		f.Message += ". Building with -tags osusergo makes os/user use the pure Go implementation in the cgo build too, so both builds resolve users the same way"
	case f.Package == "os/user" && darwinLike:
		f.Message = "darwin keeps the directory-service lookup in os/user even without cgo, so users and groups still resolve through it"
	case f.Package == "plugin":
		f.Severity, f.Code = sevError, "stub"
		f.Message = "plugin.Open always returns an error without cgo"
		f.Fix = "build with CGO_ENABLED=1 and a C compiler for this target"
	}
}
