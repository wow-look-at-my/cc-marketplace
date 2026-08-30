package main

import "strings"

// A commit can be made without any file ever existing locally: the GitHub
// contents API takes the bytes in the request body, and the createCommitOnBranch
// mutation takes them base64-encoded. Neither touches the working tree, so every
// path rule above misses them, and the result is the same -- content in the
// repository that no edit tool ever saw.
//
// These deny wherever they run. There is no "outside the tree" for a write that
// happens on the server.

func remoteWrites(seg segment, name string, rest []word) ([]write, bool) {
	switch name {
	case "gh":
		return ghAPIWrites(rest)
	case "curl", "wget", "http", "httpie":
		return httpAPIWrites(name, rest)
	}
	return nil, false
}

var ghAPIValueFlags = map[string]bool{
	"-X": true, "--method": true, "-H": true, "--header": true,
	"-f": true, "--raw-field": true, "-F": true, "--field": true,
	"-q": true, "--jq": true, "-t": true, "--template": true, "--input": true,
}

func ghAPIWrites(rest []word) ([]write, bool) {
	flags, operands := scanArgs(rest, ghAPIValueFlags)
	if len(operands) == 0 || operands[0].text != "api" {
		return nil, false
	}
	if strings.Contains(argvText(rest), "createCommitOnBranch") {
		return []write{{route: "gh api graphql createCommitOnBranch", opaque: serverSideReason}}, true
	}
	method := ""
	for _, f := range []string{"-X", "--method"} {
		if v, ok := flags[f]; ok {
			method = strings.ToUpper(v.text)
		}
	}
	if len(operands) > 1 && isContentsEndpoint(operands[1].text) && writingMethod(method) {
		return []write{{route: "gh api " + method + " " + operands[1].text, opaque: serverSideReason}}, true
	}
	return nil, true
}

func httpAPIWrites(name string, rest []word) ([]write, bool) {
	text := argvText(rest)
	if !strings.Contains(text, "api.github.com") && !strings.Contains(text, "/repos/") {
		return nil, false
	}
	if strings.Contains(text, "createCommitOnBranch") {
		return []write{{route: name + " graphql createCommitOnBranch", opaque: serverSideReason}}, true
	}
	flags, operands := scanArgs(rest, map[string]bool{
		"-X": true, "--request": true, "-H": true, "--header": true,
		"-d": true, "--data": true, "-u": true, "--user": true, "-o": true, "--output": true,
		"--method": true,
	})
	method := ""
	for _, f := range []string{"-X", "--request", "--method"} {
		if v, ok := flags[f]; ok {
			method = strings.ToUpper(v.text)
		}
	}
	for _, o := range operands {
		if isContentsEndpoint(o.text) && writingMethod(method) {
			return []write{{route: name + " " + method + " " + o.text, opaque: serverSideReason}}, true
		}
	}
	return nil, false
}

const serverSideReason = "a commit made through the GitHub API, where the file content rides in the request and never exists as a file. Edit the file with Edit or Write and push it"

func isContentsEndpoint(s string) bool {
	return strings.Contains(s, "/contents/") || strings.HasSuffix(s, "/contents")
}

func writingMethod(m string) bool {
	switch m {
	case "PUT", "POST", "PATCH", "DELETE":
		return true
	}
	return false
}

func argvText(rest []word) string {
	var b strings.Builder
	for _, a := range rest {
		b.WriteString(a.text)
		b.WriteByte(' ')
	}
	return b.String()
}
