package main

import (
	"path/filepath"
	"strings"

	"github.com/wow-look-at-my/go-containers/set"
)

// Routes found by reading the commands this environment's own permission rules
// already allow, rather than by listing writers from memory. Each was reachable
// with a rule that says "allowed" and no rule that says what it writes.

func auditedWrites(seg segment, name string, rest []word) ([]write, bool) {
	one := func(route string, paths ...word) ([]write, bool) {
		return []write{{route: route, paths: paths, dir: seg.cwd}}, true
	}

	switch name {
	case "sort":
		flags, _ := scanArgs(rest, set.Of[string]("-o", "--output", "-k", "-t", "-S", "-T"))
		for _, f := range []string{"-o", "--output"} {
			if v, ok := flags[f]; ok && v.text != "" {
				return one("sort "+f, v)
			}
		}
		return nil, true

	case "split", "csplit":
		// Output lands beside the prefix, or in the working directory when there
		// is no prefix, under names the command generates rather than names it
		// is given.
		_, operands := scanArgs(rest, set.Of[string]("-b", "-l", "-n", "-a", "-C", "--suffix-length", "--additional-suffix"))
		dir := seg.cwd
		if len(operands) > 1 {
			dir = abs(seg.cwd, filepath.Dir(operands[len(operands)-1].text))
		}
		return []write{{route: name, dir: dir, whole: true}}, true

	case "gzip", "gunzip", "bzip2", "bunzip2", "xz", "unxz", "zstd", "unzstd", "compress":
		// These replace the file they are given with a compressed or expanded
		// sibling, unless they are told to write to stdout instead.
		flags, operands := scanArgs(rest, set.Of[string]("-S", "--suffix", "-T", "--threads"))
		for _, f := range []string{"-c", "--stdout", "--to-stdout"} {
			if _, ok := flags[f]; ok {
				return nil, true
			}
		}
		if len(operands) == 0 {
			return nil, true
		}
		return one(name, operands...)

	case "zip":
		_, operands := scanArgs(rest, set.Of[string]("-x", "-i", "-t", "-n", "-b"))
		if len(operands) == 0 {
			return nil, true
		}
		return one("zip", operands[0])

	case "docker", "podman":
		// `docker cp container:/path ./local` puts a container's bytes in the
		// tree. Every other docker subcommand writes nothing here.
		_, operands := scanArgs(rest, noFlags)
		if len(operands) < 3 || operands[0].text != "cp" {
			return nil, true
		}
		dst := operands[len(operands)-1]
		if isRemoteSpec(dst.text) {
			return nil, true // the destination is inside the container
		}
		return one("docker cp", dst)
	}
	return nil, false
}

// isRemoteSpec reports whether an operand names something on another host or in
// a container -- `host:/etc/hosts`, `web:/tmp/x` -- rather than a local path. A
// Windows drive letter is a single character, and a path with a slash before the
// colon is local.
func isRemoteSpec(s string) bool {
	i := strings.IndexByte(s, ':')
	if i <= 1 {
		return false
	}
	return !strings.Contains(s[:i], "/")
}
