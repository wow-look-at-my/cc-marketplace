package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
	"mvdan.cc/sh/v3/syntax"
)

// noFlags is scanArgs' "this command has no value-taking flags" argument.
var noFlags = set.Of[string]()

// A write is one file mutation a segment would perform. Three shapes cover every
// route: a named target, a directory the write lands somewhere under, and a
// target that cannot be resolved at all.
type write struct {
	route string // how the message names the command
	paths []word // named targets, resolved against dir
	dir   string // where the write lands when paths is empty
	whole bool   // the write lands somewhere under dir rather than at a named path
	// opaque carries the reason a route's targets cannot be resolved. Such a
	// write denies wherever it runs: fail closed means an unresolvable target is
	// treated as the worst one.
	opaque string
}

// classify returns every write a segment performs. A segment naming no write
// route returns nothing and the command runs: Bash exists to run things, and
// this hook does not sandbox the programs it starts -- a build, a test run or a
// generator writes what it writes. What it does close is every route where the
// command text itself performs or directs the write.
func classify(seg segment, roots []string) []write {
	out := redirectWrites(seg)
	if len(seg.argv) == 0 {
		return out
	}
	name := commandName(seg.argv[0].text)
	rest := seg.argv[1:]

	if allowedFormatter(name, rest) {
		return out
	}
	if w, ok := gitWrites(seg, name, rest); ok {
		return append(out, w...)
	}
	if w, ok := remoteWrites(seg, name, rest); ok {
		return append(out, w...)
	}
	if w, ok := interpreterWrites(seg, name, rest, roots); ok {
		return append(out, w...)
	}
	return append(out, fileWrites(seg, name, rest, roots)...)
}

// redirectWrites covers `cmd > path`, `>>`, `>|`, `&>` and `<>`: the shell
// truncates or extends the file before the command it decorates even runs.
func redirectWrites(seg segment) []write {
	var out []write
	for _, r := range seg.redirs {
		if !writesFile(r) || isDeviceFile(r.file.text) {
			continue
		}
		op := ">"
		if r.op == syntax.AppOut || r.op == syntax.AppAll {
			op = ">>"
		}
		out = append(out, write{route: op + " " + r.file.text, paths: []word{r.file}, dir: seg.cwd})
	}
	return out
}

func writesFile(r redirTarget) bool {
	switch r.op {
	case syntax.RdrOut, syntax.AppOut, syntax.ClbOut, syntax.RdrAll, syntax.AppAll, syntax.RdrInOut:
		return true
	case syntax.DplOut:
		// `2>&1` duplicates a descriptor; `>&file` opens a file.
		t := r.file.text
		if t == "-" {
			return false
		}
		for _, c := range t {
			if c < '0' || c > '9' {
				return true
			}
		}
	}
	return false
}

func isDeviceFile(p string) bool {
	return p == "/dev/null" || p == "/dev/stdout" || p == "/dev/stderr" ||
		p == "/dev/tty" || strings.HasPrefix(p, "/dev/fd/")
}

// fileWrites is the catalog of programs that mutate a file named on their own
// argv. Each entry resolves the target rather than matching the program name and
// stopping there, so the denial can say which path it stopped.
func fileWrites(seg segment, name string, rest []word, roots []string) []write {
	one := func(route string, paths ...word) []write {
		return []write{{route: route, paths: paths, dir: seg.cwd}}
	}
	under := func(route, dir string) []write {
		return []write{{route: route, dir: dir, whole: true}}
	}

	switch name {
	case "sed":
		return sedWrites(seg, rest)
	case "awk", "gawk", "mawk", "nawk":
		return awkWrites(seg, rest)

	case "tee":
		_, operands := scanArgs(rest, noFlags)
		if len(operands) == 0 {
			return nil
		}
		return one("tee", operands...)

	case "sponge":
		_, operands := scanArgs(rest, noFlags)
		return one("sponge", operands...)

	case "dd":
		for _, a := range rest {
			if v, ok := strings.CutPrefix(a.text, "of="); ok {
				return one("dd of=", word{text: v, static: a.static})
			}
		}

	case "truncate":
		flags, operands := scanArgs(rest, set.Of[string]("-s", "--size", "-r", "--reference"))
		if _, ok := flags["-s"]; !ok {
			if _, ok := flags["--size"]; !ok {
				return nil
			}
		}
		return one("truncate", operands...)

	case "cp", "mv", "install", "rsync", "scp":
		return copyWrites(seg, name, rest, roots)

	// The five below came out of auditing the commands this environment's own
	// permission rules already allow, rather than from the list of routes anyone
	// thought of first.
	case "sort":
		flags, _ := scanArgs(rest, set.Of[string]("-o", "--output", "-k", "-t", "-S", "-T"))
		for _, f := range []string{"-o", "--output"} {
			if v, ok := flags[f]; ok && v.text != "" {
				return one("sort "+f, v)
			}
		}

	case "split", "csplit":
		// Output lands beside the prefix, or in the working directory when there
		// is no prefix, under names the command generates.
		_, operands := scanArgs(rest, set.Of[string]("-b", "-l", "-n", "-a", "-C", "--suffix-length", "--additional-suffix"))
		dir := seg.cwd
		if len(operands) > 1 {
			dir = abs(seg.cwd, filepath.Dir(operands[len(operands)-1].text))
		}
		return under(name, dir)

	case "gzip", "gunzip", "bzip2", "bunzip2", "xz", "unxz", "zstd", "unzstd", "compress":
		// These replace the file they are given with a compressed or expanded
		// sibling, unless they are told to write to stdout instead.
		flags, operands := scanArgs(rest, set.Of[string]("-S", "--suffix", "-T", "--threads"))
		if _, ok := flags["-c"]; ok {
			return nil
		}
		if _, ok := flags["--stdout"]; ok {
			return nil
		}
		if len(operands) == 0 {
			return nil
		}
		return one(name, operands...)

	case "zip":
		_, operands := scanArgs(rest, set.Of[string]("-x", "-i", "-t", "-n", "-b"))
		if len(operands) == 0 {
			return nil
		}
		return one("zip", operands[0])

	case "docker", "podman":
		// `docker cp container:/path ./local` puts a container's bytes in the
		// tree. Every other docker subcommand writes nothing here.
		_, operands := scanArgs(rest, noFlags)
		if len(operands) < 3 || operands[0].text != "cp" {
			return nil
		}
		dst := operands[len(operands)-1]
		if strings.Contains(dst.text, ":") {
			return nil // the destination is inside a container
		}
		return one("docker cp", dst)

	case "ln":
		// A symlink replaces the path it is created at, so the link name is the
		// write -- pointing a tracked path at writable storage changes what the
		// tree holds without any tool seeing it.
		flags, operands := scanArgs(rest, set.Of[string]("-t", "--target-directory"))
		if v, ok := flags["-t"]; ok {
			return under("ln -t", abs(seg.cwd, v.text))
		}
		if v, ok := flags["--target-directory"]; ok {
			return under("ln --target-directory", abs(seg.cwd, v.text))
		}
		switch len(operands) {
		case 0:
			return nil
		case 1:
			return one("ln", word{text: filepath.Base(operands[0].text), static: operands[0].static})
		case 2:
			return one("ln", operands[1])
		default:
			return under("ln", abs(seg.cwd, operands[len(operands)-1].text))
		}

	case "patch":
		// The files a patch touches are named inside the patch, not on the argv,
		// so the write lands somewhere under the directory patch runs in.
		flags, _ := scanArgs(rest, set.Of[string]("-o", "--output", "-d", "--directory", "-i", "--input", "-p", "--strip", "-B", "-D", "-r", "-z"))
		if v, ok := flags["-o"]; ok {
			return one("patch -o", v)
		}
		if v, ok := flags["--output"]; ok {
			return one("patch --output", v)
		}
		dir := seg.cwd
		if v, ok := flags["-d"]; ok {
			dir = abs(seg.cwd, v.text)
		} else if v, ok := flags["--directory"]; ok {
			dir = abs(seg.cwd, v.text)
		}
		return under("patch", dir)

	case "tar":
		return tarWrites(seg, rest)

	case "unzip":
		flags, _ := scanArgs(rest, set.Of[string]("-d", "-x"))
		dir := seg.cwd
		if v, ok := flags["-d"]; ok {
			dir = abs(seg.cwd, v.text)
		}
		return under("unzip", dir)

	case "xxd":
		flags, operands := scanArgs(rest, set.Of[string]("-c", "-g", "-l", "-o", "-s"))
		if _, ok := flags["-r"]; !ok {
			if _, ok := flags["--revert"]; !ok {
				return nil
			}
		}
		if len(operands) >= 2 {
			return one("xxd -r", operands[1])
		}

	case "base64", "openssl":
		flags, _ := scanArgs(rest, set.Of[string]("-o", "--output", "-out"))
		for _, f := range []string{"-o", "--output", "-out"} {
			if v, ok := flags[f]; ok && v.text != "" {
				return one(name+" "+f, v)
			}
		}

	case "curl":
		return curlWrites(seg, rest)

	case "wget":
		flags, _ := scanArgs(rest, set.Of[string]("-O", "--output-document", "-P", "--directory-prefix"))
		if v, ok := flags["-O"]; ok {
			return one("wget -O", v)
		}
		if v, ok := flags["--output-document"]; ok {
			return one("wget -O", v)
		}
		dir := seg.cwd
		if v, ok := flags["-P"]; ok {
			dir = abs(seg.cwd, v.text)
		} else if v, ok := flags["--directory-prefix"]; ok {
			dir = abs(seg.cwd, v.text)
		}
		return under("wget", dir)

	case "gh":
		return ghWrites(seg, rest)
	}

	return inPlaceRewrite(seg, name, rest)
}

// copyWrites is where the rule's real shape shows: this is about content
// ENTERING the tree without a tool call. Bytes already in the tree have been
// through one, so moving or copying them around it -- `mv old.go new.go` -- is
// ordinary refactoring. A source from outside is the splice this closes: write a
// file to /tmp with Write, then move it over the target.
func copyWrites(seg segment, name string, rest []word, roots []string) []write {
	flags, operands := scanArgs(rest, set.Of[string](
		"-t", "--target-directory", "-S", "--suffix",
		"-m", "--mode", "-o", "--owner", "-g", "--group",
		"-e", "--rsh", "--exclude", "--include", "--files-from",
	))
	if v, ok := flags["-t"]; ok {
		if fromInsideTree(seg.cwd, operands, roots) {
			return nil
		}
		return []write{{route: name + " -t", dir: abs(seg.cwd, v.text), whole: true}}
	}
	if v, ok := flags["--target-directory"]; ok {
		if fromInsideTree(seg.cwd, operands, roots) {
			return nil
		}
		return []write{{route: name + " --target-directory", dir: abs(seg.cwd, v.text), whole: true}}
	}
	if len(operands) < 2 {
		return nil
	}
	if fromInsideTree(seg.cwd, operands[:len(operands)-1], roots) {
		return nil
	}
	dst := operands[len(operands)-1]
	// rsync's trailing slash, several sources, or an existing directory all mean
	// the destination is a directory and the write lands under it by basename.
	if len(operands) > 2 || strings.HasSuffix(dst.text, "/") {
		return []write{{route: name, dir: abs(seg.cwd, dst.text), whole: true}}
	}
	return []write{{route: name, paths: []word{dst}, dir: seg.cwd}}
}

func tarWrites(seg segment, rest []word) []write {
	flags, _ := scanArgs(rest, set.Of[string]("-f", "--file", "-C", "--directory"))
	has := func(names ...string) bool {
		for _, n := range names {
			if _, ok := flags[n]; ok {
				return true
			}
		}
		return false
	}
	dir := seg.cwd
	if v, ok := flags["-C"]; ok {
		dir = abs(seg.cwd, v.text)
	} else if v, ok := flags["--directory"]; ok {
		dir = abs(seg.cwd, v.text)
	}
	if has("-x", "--extract", "--get") {
		return []write{{route: "tar -x", dir: dir, whole: true}}
	}
	if has("-c", "--create") {
		if v, ok := flags["-f"]; ok {
			return []write{{route: "tar -f", paths: []word{v}, dir: seg.cwd}}
		}
		if v, ok := flags["--file"]; ok {
			return []write{{route: "tar --file", paths: []word{v}, dir: seg.cwd}}
		}
	}
	return nil
}

func curlWrites(seg segment, rest []word) []write {
	flags, _ := scanArgs(rest, set.Of[string](
		"-o", "--output", "--output-dir", "-H", "--header",
		"-d", "--data", "-X", "--request", "-u", "--user",
		"-A", "--user-agent", "-b", "--cookie", "-c", "--cookie-jar",
		"-w", "--write-out", "-D", "--dump-header", "-T", "--upload-file",
	))
	var out []write
	for _, f := range []string{"-o", "--output", "--cookie-jar", "-c", "-D", "--dump-header"} {
		if v, ok := flags[f]; ok && v.text != "" && !isDeviceFile(v.text) {
			out = append(out, write{route: "curl " + f, paths: []word{v}, dir: seg.cwd})
		}
	}
	if _, ok := flags["-O"]; ok {
		dir := seg.cwd
		if v, ok := flags["--output-dir"]; ok {
			dir = abs(seg.cwd, v.text)
		}
		out = append(out, write{route: "curl -O", dir: dir, whole: true})
	}
	return out
}

func ghWrites(seg segment, rest []word) []write {
	_, operands := scanArgs(rest, noFlags)
	if len(operands) < 2 || operands[0].text != "release" || operands[1].text != "download" {
		return nil
	}
	flags, _ := scanArgs(rest, set.Of[string]("-O", "--output", "-D", "--dir", "-p", "--pattern", "-A", "--archive", "-R", "--repo"))
	if v, ok := flags["-O"]; ok {
		return []write{{route: "gh release download -O", paths: []word{v}, dir: seg.cwd}}
	}
	if v, ok := flags["--output"]; ok {
		return []write{{route: "gh release download -O", paths: []word{v}, dir: seg.cwd}}
	}
	dir := seg.cwd
	if v, ok := flags["-D"]; ok {
		dir = abs(seg.cwd, v.text)
	} else if v, ok := flags["--dir"]; ok {
		dir = abs(seg.cwd, v.text)
	}
	return []write{{route: "gh release download", dir: dir, whole: true}}
}

// inPlaceRewrite is the rule for a program this catalog does not name. A long
// in-place flag says plainly that the program rewrites the files it is given,
// whatever the program is, so the tool does not have to be recognised first --
// which is what stops the catalog from leaking every time a new one appears.
// Short `-i` and `-w` are ambiguous (`grep -w`, `curl -w`), so they count only
// for the tools known to spell in-place that way.
func inPlaceRewrite(seg segment, name string, rest []word) []write {
	longFlag := false
	shortFlag := false
	for _, a := range rest {
		t := a.text
		switch {
		case t == "--in-place" || t == "--write" || strings.HasPrefix(t, "--in-place="):
			longFlag = true
		case t == "-i" || t == "-w":
			shortFlag = true
		}
	}
	if !longFlag && !(shortFlag && shortInPlaceTools.Contains(name)) {
		return nil
	}
	_, operands := scanArgs(rest, noFlags)
	// An unrecognised tool's operands hold its subcommand as well as its files
	// (`ffs fmt -w x.ffs`), so only the ones that look like paths are reported --
	// a denial naming "fmt" tells the reader nothing.
	var targets []word
	for _, o := range operands {
		if looksLikePath(seg.cwd, o) {
			targets = append(targets, o)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	return []write{{route: name + " (in-place rewrite)", paths: targets, dir: seg.cwd}}
}

// fromInsideTree reports whether every source is content the tree already holds.
// An unknowable source is not, which is what keeps `cp $SRC tracked.go` denied.
func fromInsideTree(cwd string, sources []word, roots []string) bool {
	if len(sources) == 0 {
		return false
	}
	for _, s := range sources {
		if !s.static {
			return false
		}
		p := abs(cwd, s.text)
		if p == "" {
			return false
		}
		if _, inside := insideGuarded(roots, p); !inside {
			return false
		}
	}
	return true
}

func looksLikePath(cwd string, o word) bool {
	if !o.static {
		return true // unknowable, and unknowable denies
	}
	if strings.ContainsAny(o.text, "/.") {
		return true
	}
	if p := abs(cwd, o.text); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// Tools that spell in-place rewriting with a bare short flag. Membership here
// only decides that the flag is read as in-place; whether the rewrite is allowed
// is the formatter table's decision.
var shortInPlaceTools = set.Of[string]("gofmt", "goimports", "shfmt", "ffs",
	"rustfmt", "clang-format", "buf", "yq")

// scanArgs splits an argv into its flags and its operands. A flag named in
// valueFlags consumes the next word, which is what keeps `head -n 20 file` from
// reading 20 as a path. Bundled short flags each register on their own.
func scanArgs(rest []word, valueFlags set.Set[string]) (map[string]word, []word) {
	flags := map[string]word{}
	var operands []word
	dashDash := false
	for i := 0; i < len(rest); i++ {
		t := rest[i].text
		switch {
		case dashDash:
			operands = append(operands, rest[i])
		case t == "--":
			dashDash = true
		case strings.HasPrefix(t, "--"):
			if k, v, ok := strings.Cut(t, "="); ok {
				flags[k] = word{text: v, static: rest[i].static}
				continue
			}
			if valueFlags.Contains(t) && i+1 < len(rest) {
				i++
				flags[t] = rest[i]
				continue
			}
			flags[t] = word{static: true}
		case len(t) > 1 && strings.HasPrefix(t, "-"):
			if valueFlags.Contains(t) && i+1 < len(rest) {
				i++
				flags[t] = rest[i]
				continue
			}
			flags[t] = word{text: t[1:], static: rest[i].static}
			for _, c := range t[1:] {
				short := "-" + string(c)
				if _, seen := flags[short]; !seen {
					flags[short] = word{static: true}
				}
			}
		default:
			operands = append(operands, rest[i])
		}
	}
	return flags, operands
}
