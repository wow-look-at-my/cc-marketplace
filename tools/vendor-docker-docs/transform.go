package main

import (
	"fmt"
	"regexp"
	"strings"
)

// stripFrontmatter removes a leading YAML frontmatter block and returns the
// body plus the block's `title`, which becomes the page's H1. Hugo renders the
// title from frontmatter, so a page whose block is dropped otherwise arrives
// with no heading at all.
func stripFrontmatter(src string) (title, body string) {
	const fence = "---\n"
	if !strings.HasPrefix(src, fence) {
		return "", src
	}
	rest := src[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return "", src
	}
	block := rest[:end]
	body = rest[end+len("\n")+len(fence):]

	for _, line := range strings.Split(block, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(key) != "title" {
			continue
		}
		title = strings.Trim(strings.TrimSpace(value), `"'`)
		break
	}
	return title, body
}

var includePattern = regexp.MustCompile(`\{\{%\s*include\s+"([^"]+)"\s*%\}\}`)

// resolveIncludes inlines every `{{% include "compose/x.md" %}}`. The partial's
// own frontmatter is dropped; its title is not used, because the partial is
// spliced into the middle of a page that already has one.
//
// fetch returns the partial's raw text for a path under includeRoot. A partial
// may itself include another, so this recurses; depth is bounded because an
// include cycle would otherwise hang.
func resolveIncludes(body string, fetch func(path string) (string, error), depth int) (string, error) {
	if depth > 8 {
		return "", fmt.Errorf("include nesting deeper than 8 levels; likely a cycle")
	}
	var err error
	out := includePattern.ReplaceAllStringFunc(body, func(match string) string {
		if err != nil {
			return match
		}
		name := includePattern.FindStringSubmatch(match)[1]
		raw, fetchErr := fetch(includeRoot + name)
		if fetchErr != nil {
			err = fmt.Errorf("include %q: %w", name, fetchErr)
			return match
		}
		_, partial := stripFrontmatter(raw)
		nested, nestedErr := resolveIncludes(partial, fetch, depth+1)
		if nestedErr != nil {
			err = nestedErr
			return match
		}
		return strings.TrimRight(nested, "\n")
	})
	return out, err
}

var (
	summaryBar    = regexp.MustCompile(`\{\{<\s*summary-bar\s+feature_name="([^"]*)"\s*>\}\}`)
	anyShortcode  = regexp.MustCompile(`\{\{[<%][^}]*[>%]\}\}`)
	rootRelLink   = regexp.MustCompile(`\]\((/[^)]*)\)`)
	dockerDocsURL = "https://docs.docker.com"
)

// stripShortcodes removes the Hugo shortcodes that survive include resolution.
//
// A summary-bar renders a badge saying which product version first shipped the
// feature. The version itself lives in a Hugo data file this tool does not
// read, so the badge becomes a line naming the feature: dropping it silently
// would delete the only signal that the option is version-gated at all.
//
// Any OTHER shortcode is an error. Upstream adding one is exactly the change
// that must not pass through unnoticed, either as literal Hugo syntax in the
// output or as a silent deletion.
func stripShortcodes(body string) (string, error) {
	body = summaryBar.ReplaceAllString(body, "> Version-gated feature: \"$1\". Check upstream for the minimum version.")

	if leftover := anyShortcode.FindString(body); leftover != "" {
		return "", fmt.Errorf("unhandled Hugo shortcode %q: teach stripShortcodes about it", leftover)
	}
	return body, nil
}

// absolutizeLinks rewrites Hugo's root-relative links to absolute docs.docker.com
// URLs. Vendored out of the site, `](/reference/cli/docker/)` resolves against
// whatever host reads the file, which is never the right one.
//
// An anchor, an absolute URL and a same-directory relative link are all left
// alone: the first two already work, and the third points at a sibling file
// this tool vendors next to it.
func absolutizeLinks(body string) string {
	return rootRelLink.ReplaceAllString(body, "]("+dockerDocsURL+"$1)")
}

// header is the provenance block prepended to every vendored file. It names the
// exact commit read, so a reader can diff this copy against upstream, and it
// carries the license the content is used under.
func header(title string, p page, commit string) string {
	var b strings.Builder
	if title != "" {
		fmt.Fprintf(&b, "# %s\n\n", title)
	}
	b.WriteString("<!-- Vendored file. Do not edit by hand. -->\n")
	fmt.Fprintf(&b, "> Vendored verbatim from [`%s`](%s) at commit `%s`.\n",
		p.Src.Repo+"/"+p.Path, fmt.Sprintf(p.Src.Blob, commit, p.Path), commit)
	fmt.Fprintf(&b, "> Licensed %s. Regenerate with `go run ./tools/vendor-docker-docs`.\n\n", p.Src.License)
	return b.String()
}

// render turns one fetched page into the file written to disk.
func render(raw string, p page, commit string, fetch func(string) (string, error)) (string, error) {
	title, body := stripFrontmatter(raw)

	body, err := resolveIncludes(body, fetch, 0)
	if err != nil {
		return "", err
	}
	if body, err = stripShortcodes(body); err != nil {
		return "", err
	}
	body = absolutizeLinks(body)

	return header(title, p, commit) + strings.TrimLeft(body, "\n"), nil
}
