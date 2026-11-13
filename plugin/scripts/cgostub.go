// Command cgostub reports the Go packages that change when a program is built
// with CGO_ENABLED=0: dependencies that compile a stub in place of their cgo
// code, switch to a different implementation, leave code out, or stop
// building. It compares go list for the same target with and without cgo,
// and confirms the build without cgo with a real go build.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const version = "0.1.0"

const usage = `usage: cgostub [flags] [packages]

Reports the packages that change when the named packages (default ".") are
built with CGO_ENABLED=0 instead of CGO_ENABLED=1: stubs, fallbacks, code that
is left out, and packages that don't build at all.

Flags:
`

type options struct {
	targets  []string
	tags     string
	format   string
	failOn   string
	std      bool
	dir      string
	patterns []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cgostub", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	target := fs.String("target", "", "comma-separated GOOS/GOARCH pairs to check, such as linux/amd64,darwin/arm64 (default: go env GOOS/GOARCH)")
	tags := fs.String("tags", "", "build tags, as for go build -tags")
	format := fs.String("format", "text", "output format: text or json")
	failOn := fs.String("fail-on", "error", "exit 1 on findings at or above this severity: error, warn or never")
	std := fs.Bool("std", false, "also list standard library packages that change without cgo")
	dir := fs.String("dir", ".", "directory to run go list and go build in")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "cgostub "+version)
		return 0
	}
	opts := options{tags: *tags, format: *format, failOn: *failOn, std: *std, dir: *dir, patterns: fs.Args()}
	if opts.format != "text" && opts.format != "json" {
		fmt.Fprintf(stderr, "cgostub: --format must be text or json, not %q\n", opts.format)
		return 2
	}
	if opts.failOn != sevError && opts.failOn != sevWarn && opts.failOn != "never" {
		fmt.Fprintf(stderr, "cgostub: --fail-on must be error, warn or never, not %q\n", opts.failOn)
		return 2
	}
	if len(opts.patterns) == 0 {
		opts.patterns = []string{"."}
	}
	for _, p := range opts.patterns {
		if strings.HasPrefix(p, "-") {
			fmt.Fprintf(stderr, "cgostub: flags go before the packages: %q\n", p)
			return 2
		}
	}
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Fprintln(stderr, "cgostub: can't trust the input, nothing was checked: the go command is not on PATH")
		return 2
	}
	if *target != "" {
		for _, t := range strings.Split(*target, ",") {
			t = strings.TrimSpace(t)
			goos, goarch, ok := strings.Cut(t, "/")
			if !ok || goos == "" || goarch == "" || strings.Contains(goarch, "/") {
				fmt.Fprintf(stderr, "cgostub: --target wants GOOS/GOARCH pairs such as linux/amd64, not %q\n", t)
				return 2
			}
			opts.targets = append(opts.targets, t)
		}
	} else {
		host, err := goEnv(opts.dir, "", "", "GOOS", "GOARCH")
		if err != nil {
			return fail(stderr, err)
		}
		opts.targets = []string{host[0] + "/" + host[1]}
	}

	tmp, err := os.MkdirTemp("", "cgostub-")
	if err != nil {
		fmt.Fprintf(stderr, "cgostub: %v\n", err)
		return 2
	}
	defer os.RemoveAll(tmp)

	host, err := goEnv(opts.dir, "", "", "GOOS", "GOARCH", "CGO_ENABLED")
	if err != nil {
		return fail(stderr, err)
	}
	var reports []*targetReport
	for _, t := range opts.targets {
		goos, goarch, _ := strings.Cut(t, "/")
		// The packages are also built with cgo when that can work here: the
		// target is this machine and go found a C compiler.
		cgoBuild := goos == host[0] && goarch == host[1] && host[2] == "1"
		rep, err := checkTarget(opts.dir, goos, goarch, opts.tags, opts.patterns, tmp, cgoBuild)
		if err != nil {
			return fail(stderr, err)
		}
		def, err := goEnv(opts.dir, goos, goarch, "CGO_ENABLED")
		if err != nil {
			return fail(stderr, err)
		}
		rep.DefaultCgo = def[0]
		rep.host = host[0] + "/" + host[1]
		reports = append(reports, rep)
	}

	errs, warns := 0, 0
	for _, r := range reports {
		for _, f := range r.Findings {
			switch f.Severity {
			case sevError:
				errs++
			case sevWarn:
				warns++
			}
		}
	}
	if opts.format == "json" {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(struct {
			Version  string          `json:"version"`
			Errors   int             `json:"errors"`
			Warnings int             `json:"warnings"`
			Targets  []*targetReport `json:"targets"`
		}{version, errs, warns, reports})
	} else {
		writeText(stdout, reports, opts.std)
	}
	switch {
	case opts.failOn == sevError && errs > 0, opts.failOn == sevWarn && errs+warns > 0:
		return 1
	}
	return 0
}

func fail(stderr io.Writer, err error) int {
	if u, ok := err.(*untrusted); ok {
		fmt.Fprintf(stderr, "cgostub: can't trust the input, nothing was checked: %s\n", u.msg)
	} else {
		fmt.Fprintf(stderr, "cgostub: %v\n", err)
	}
	return 2
}

func writeText(w io.Writer, reports []*targetReport, std bool) {
	for _, r := range reports {
		shown := r.Findings
		if std {
			shown = append(append([]finding{}, r.Findings...), r.Standard...)
		}
		for _, f := range shown {
			label := map[string]string{sevError: "ERROR", sevWarn: "WARN", sevNote: "note"}[f.Severity]
			fmt.Fprintf(w, "%s %s: %s, for %s\n", label, f.Code, f.Package, r.Target)
			fmt.Fprintf(w, "  %s\n", f.Message)
			if len(f.Via) > 0 {
				fmt.Fprintf(w, "  imported by %s\n", strings.Join(f.Via, " -> "))
			}
			if f.Fix != "" {
				fmt.Fprintf(w, "  fix: %s\n", f.Fix)
			}
			fmt.Fprintln(w)
		}
	}
	for _, r := range reports {
		e, wn := 0, 0
		for _, f := range r.Findings {
			if f.Severity == sevError {
				e++
			} else if f.Severity == sevWarn {
				wn++
			}
		}
		var line string
		if e+wn == 0 {
			line = fmt.Sprintf("%s: nothing outside the standard library changes without cgo (%s checked).", r.Target, plural(r.NonStandard, "package", "packages"))
		} else {
			line = fmt.Sprintf("%s: %s and %s in %s outside the standard library.", r.Target, plural(e, "error", "errors"), plural(wn, "warning", "warnings"), plural(r.NonStandard, "package", "packages"))
		}
		fmt.Fprintf(w, "%s go env CGO_ENABLED for %s here is %s.\n", line, r.Target, r.DefaultCgo)
		if len(r.Standard) > 0 && !std {
			var names []string
			for _, f := range r.Standard {
				names = append(names, f.Package)
			}
			fmt.Fprintf(w, "  Standard library packages that also change: %s (--std explains).\n", strings.Join(names, ", "))
		}
		switch {
		case !r.BuiltWithoutCgo:
			fmt.Fprintf(w, "  The build without cgo wasn't run, because go list already shows a package that can't build. Compile errors in other packages may be hidden.\n")
		case !r.BuiltWithCgo && r.Target == r.host:
			fmt.Fprintf(w, "  Built without cgo only: go env CGO_ENABLED is 0 here (no C compiler found, or CGO_ENABLED=0 is set), so a build with cgo wasn't tried.\n")
		case !r.BuiltWithCgo:
			fmt.Fprintf(w, "  Built without cgo only: a build with cgo for %s needs a C compiler for that target, so it wasn't tried.\n", r.Target)
		}
	}
}
