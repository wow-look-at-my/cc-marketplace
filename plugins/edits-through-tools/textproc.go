package main

import "strings"

// sed and awk are the two filters that can write a file without being told to on
// the argv: sed's `w` command and awk's `print >` both name their target inside
// the program text. Reading the program is what separates a filter (`awk '$1 >
// 5'` writes nothing) from a writer, so neither is denied for its name alone.

func sedWrites(seg segment, rest []word) []write {
	valueFlags := map[string]bool{"-e": true, "--expression": true, "-f": true, "--file": true, "-l": true, "--line-length": true}
	flags, operands := scanArgs(rest, valueFlags)

	inPlace := false
	for _, a := range rest {
		t := a.text
		if t == "--in-place" || strings.HasPrefix(t, "--in-place=") {
			inPlace = true
			continue
		}
		if !strings.HasPrefix(t, "-") || strings.HasPrefix(t, "--") {
			continue
		}
		// `-i`, `-i.bak` and a cluster like `-ri` all mean in-place. A cluster
		// ends at the first letter that takes a value, because everything after
		// it is that value.
		for _, c := range t[1:] {
			if c == 'i' {
				inPlace = true
				break
			}
			if c == 'e' || c == 'f' || c == 'l' {
				break
			}
		}
	}

	scripts, files := sedScriptAndFiles(flags, operands)
	var out []write
	if inPlace && len(files) > 0 {
		out = append(out, write{route: "sed -i", paths: files, dir: seg.cwd})
	}
	for _, s := range scripts {
		targets, unresolvable := sedScriptTargets(s.text)
		if unresolvable || !s.static {
			out = append(out, write{route: "sed", opaque: "a sed script whose w command names a file this hook cannot resolve"})
			continue
		}
		for _, t := range targets {
			out = append(out, write{route: "sed w", paths: []word{{text: t, static: true}}, dir: seg.cwd})
		}
	}
	return out
}

// sedScriptAndFiles separates the program from the files. Without -e or -f the
// first operand is the program, which is what keeps `sed s/a/b/ f` from reading
// its own script as a path.
func sedScriptAndFiles(flags map[string]word, operands []word) (scripts, files []word) {
	explicit := false
	for _, f := range []string{"-e", "--expression"} {
		if v, ok := flags[f]; ok {
			scripts = append(scripts, v)
			explicit = true
		}
	}
	if _, ok := flags["-f"]; ok {
		explicit = true // the program is in a file this hook does not read
	}
	if _, ok := flags["--file"]; ok {
		explicit = true
	}
	if !explicit && len(operands) > 0 {
		return []word{operands[0]}, operands[1:]
	}
	return scripts, operands
}

// sedScriptTargets finds the files a sed program writes with `w`, `W` or the `w`
// flag on an s command. An empty filename after one of those is a program this
// hook cannot read, which the caller turns into a denial.
func sedScriptTargets(script string) (targets []string, unresolvable bool) {
	for _, piece := range splitSedCommands(script) {
		body := strings.TrimLeft(dropSedAddress(piece), " \t")
		if body == "" {
			continue
		}
		switch body[0] {
		case 'w', 'W':
			name := strings.TrimSpace(body[1:])
			if name == "" {
				unresolvable = true
				continue
			}
			targets = append(targets, name)
		case 's', 'y':
			name, ok, found := sCommandWriteTarget(body)
			if !found {
				continue
			}
			if !ok {
				unresolvable = true
				continue
			}
			targets = append(targets, name)
		}
	}
	return targets, unresolvable
}

// sCommandWriteTarget reads the flags of an s command and returns the filename
// its `w` flag names.
func sCommandWriteTarget(body string) (name string, resolved, found bool) {
	if len(body) < 2 {
		return "", false, false
	}
	delim := body[1]
	fields := 0
	i := 2
	for ; i < len(body) && fields < 2; i++ {
		if body[i] == '\\' {
			i++
			continue
		}
		if body[i] == delim {
			fields++
		}
	}
	if fields < 2 {
		return "", false, false
	}
	for ; i < len(body); i++ {
		c := body[i]
		if c == 'w' {
			n := strings.TrimSpace(body[i+1:])
			if n == "" {
				return "", false, true
			}
			return n, true, true
		}
		if c >= '0' && c <= '9' {
			continue
		}
		if c != 'g' && c != 'i' && c != 'I' && c != 'm' && c != 'M' && c != 'p' && c != 'e' {
			return "", false, false
		}
	}
	return "", false, false
}

// splitSedCommands breaks a program at the separators sed itself uses. A `w`
// filename runs to the end of its line, so a newline is the only separator that
// can follow one.
func splitSedCommands(script string) []string {
	var out []string
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimLeft(line, " \t;{")
		if strings.HasPrefix(trimmed, "w") || strings.HasPrefix(trimmed, "W") {
			out = append(out, trimmed)
			continue
		}
		for _, piece := range strings.Split(line, ";") {
			out = append(out, strings.TrimLeft(piece, " \t{}"))
		}
	}
	return out
}

func dropSedAddress(piece string) string {
	i := 0
	for i < len(piece) {
		c := piece[i]
		switch {
		case c == ' ' || c == '\t' || c == ',' || c == '$' || c == '!' || (c >= '0' && c <= '9'):
			i++
		case c == '/':
			i++
			for i < len(piece) && piece[i] != '/' {
				if piece[i] == '\\' {
					i++
				}
				i++
			}
			i++
		default:
			return piece[i:]
		}
	}
	return ""
}

func awkWrites(seg segment, rest []word) []write {
	valueFlags := map[string]bool{"-v": true, "--assign": true, "-f": true, "--file": true, "-F": true, "--field-separator": true, "-i": true, "--include": true}
	flags, operands := scanArgs(rest, valueFlags)

	// gawk spells in-place editing as loading its inplace library, `-i inplace`,
	// which scanArgs reads as the value of -i. A bare -i naming any other library
	// is not in-place editing.
	inPlace := false
	for _, f := range []string{"-i", "--include"} {
		if v, ok := flags[f]; ok && v.text == "inplace" {
			inPlace = true
		}
	}
	for _, a := range rest {
		if a.text == "--in-place" {
			inPlace = true
		}
	}

	var program word
	files := operands
	progFromFile := false
	for _, f := range []string{"-f", "--file"} {
		if _, ok := flags[f]; ok {
			progFromFile = true
		}
	}
	if !progFromFile && len(operands) > 0 {
		program, files = operands[0], operands[1:]
	}

	var out []write
	if inPlace && len(files) > 0 {
		out = append(out, write{route: "awk -i inplace", paths: files, dir: seg.cwd})
	}
	if progFromFile || program.text == "" {
		return out
	}
	if !program.static {
		return append(out, write{route: "awk", opaque: "an awk program assembled from an expansion, whose redirects cannot be resolved"})
	}
	targets, unresolvable := awkRedirectTargets(program.text)
	if unresolvable {
		out = append(out, write{route: "awk", opaque: "an awk program whose print redirect names a file this hook cannot resolve"})
	}
	for _, t := range targets {
		out = append(out, write{route: "awk print >", paths: []word{{text: t, static: true}}, dir: seg.cwd})
	}
	return out
}

// awkRedirectTargets finds the files an awk program writes. In awk's grammar an
// unparenthesised `>` after print or printf is a redirect, and anywhere else it
// is a comparison -- which is why `awk '$1 > 5'` is not a writer and
// `awk '{print > "f"}'` is.
func awkRedirectTargets(prog string) (targets []string, unresolvable bool) {
	printSeen := false
	inString := false
	for i := 0; i < len(prog); i++ {
		c := prog[i]
		if inString {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
		case c == ';' || c == '}' || c == '{' || c == '\n':
			printSeen = false
		case c == '>' && printSeen:
			if i+1 < len(prog) && prog[i+1] == '>' {
				i++
			}
			name, ok := awkLiteralAfter(prog[i+1:])
			if !ok {
				unresolvable = true
				continue
			}
			if !isDeviceFile(name) {
				targets = append(targets, name)
			}
		case isWordStart(c):
			word, next := readWord(prog, i)
			i = next - 1
			if word == "print" || word == "printf" {
				printSeen = true
			}
		}
	}
	return targets, unresolvable
}

func awkLiteralAfter(s string) (string, bool) {
	s = strings.TrimLeft(s, " \t")
	if !strings.HasPrefix(s, `"`) {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			b.WriteByte(s[i])
			continue
		}
		if s[i] == '"' {
			return b.String(), true
		}
		b.WriteByte(s[i])
	}
	return "", false
}

func isWordStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

func readWord(s string, i int) (string, int) {
	j := i
	for j < len(s) && (isWordStart(s[j]) || s[j] >= '0' && s[j] <= '9') {
		j++
	}
	return s[i:j], j
}
