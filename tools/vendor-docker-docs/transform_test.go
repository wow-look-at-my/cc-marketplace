package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wow-look-at-my/go-containers/set"
)

func TestStripFrontmatterTakesTheTitleAndDropsTheBlock(t *testing.T) {
	title, body := stripFrontmatter("---\ntitle: Services top-level element\nweight: 20\n---\n\nA service is...\n")

	assert.Equal(t, "Services top-level element", title)
	assert.Equal(t, "\nA service is...\n", body)
}

func TestStripFrontmatterLeavesAFileThatHasNone(t *testing.T) {
	title, body := stripFrontmatter("# Dockerfile reference\n\nText.\n")

	assert.Empty(t, title)
	assert.Equal(t, "# Dockerfile reference\n\nText.\n", body)
}

// A horizontal rule at the top of a body is not a frontmatter fence. Treating
// it as one would swallow everything up to the next rule.
func TestStripFrontmatterLeavesAnUnterminatedBlockAlone(t *testing.T) {
	src := "---\nnot really frontmatter\n"
	title, body := stripFrontmatter(src)

	assert.Empty(t, title)
	assert.Equal(t, src, body)
}

func TestResolveIncludesInlinesThePartial(t *testing.T) {
	fetch := func(path string) (string, error) {
		require.Equal(t, "content/includes/compose/services.md", path)
		return "---\ntitle: ignored\n---\nA service is an abstract definition.\n", nil
	}

	out, err := resolveIncludes("Intro.\n\n{{% include \"compose/services.md\" %}}\n\nOutro.\n", fetch, 0)

	require.NoError(t, err)
	assert.Equal(t, "Intro.\n\nA service is an abstract definition.\n\nOutro.\n", out)
}

// The partial's own title is dropped: it is spliced into a page that already
// has an H1, so keeping it would produce two.
func TestResolveIncludesDropsThePartialsFrontmatter(t *testing.T) {
	fetch := func(string) (string, error) { return "---\ntitle: Partial\n---\nBody.\n", nil }

	out, err := resolveIncludes("{{% include \"compose/x.md\" %}}\n", fetch, 0)

	require.NoError(t, err)
	assert.NotContains(t, out, "Partial")
	assert.Contains(t, out, "Body.")
}

func TestResolveIncludesFailsOnACycle(t *testing.T) {
	fetch := func(string) (string, error) { return "{{% include \"compose/loop.md\" %}}", nil }

	_, err := resolveIncludes("{{% include \"compose/loop.md\" %}}", fetch, 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestResolveIncludesReportsAFetchFailure(t *testing.T) {
	fetch := func(string) (string, error) { return "", fmt.Errorf("404") }

	_, err := resolveIncludes("{{% include \"compose/gone.md\" %}}", fetch, 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "compose/gone.md")
}

// The badge carries the fact that the option is version-gated. Dropping it
// silently would delete the only signal that a field is not universally available.
func TestStripShortcodesKeepsTheVersionGateSignal(t *testing.T) {
	out, err := stripShortcodes("### gpus\n\n{{< summary-bar feature_name=\"Compose gpus\" >}}\n\nText.\n")

	require.NoError(t, err)
	assert.Contains(t, out, "Version-gated feature: \"Compose gpus\"")
	assert.NotContains(t, out, "{{<")
}

// An upstream shortcode nobody taught this tool about must stop the run, rather
// than leaking Hugo syntax into the reference or vanishing without a trace.
func TestStripShortcodesRejectsAnUnknownShortcode(t *testing.T) {
	_, err := stripShortcodes("{{< grid >}}\n")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "grid")
}

func TestAbsolutizeLinksRewritesOnlyRootRelativeOnes(t *testing.T) {
	in := "See [cli](/reference/cli/docker/), [anchor](#build), " +
		"[abs](https://example.com/x), and [sibling](services.md)."

	out := absolutizeLinks(in)

	assert.Contains(t, out, "(https://docs.docker.com/reference/cli/docker/)")
	assert.Contains(t, out, "(#build)")
	assert.Contains(t, out, "(https://example.com/x)")
	assert.Contains(t, out, "(services.md)")
}

func TestRenderProducesAttributedOutput(t *testing.T) {
	p := composePage("services", "services")
	raw := "---\ntitle: Services\n---\n\n{{% include \"compose/services.md\" %}}\n\n" +
		"{{< summary-bar feature_name=\"Compose gpus\" >}}\n\nSee [cli](/reference/cli/).\n"
	fetch := func(string) (string, error) { return "A service is an abstract definition.\n", nil }

	out, err := render(raw, p, "0123456789abcdef0123456789abcdef01234567", fetch)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(out, "# Services\n"), "title becomes the H1")
	assert.Contains(t, out, "Do not edit by hand")
	assert.Contains(t, out, "0123456789abcdef0123456789abcdef01234567")
	assert.Contains(t, out, "Licensed Apache-2.0")
	assert.Contains(t, out, "A service is an abstract definition.")
	assert.Contains(t, out, "https://docs.docker.com/reference/cli/")
	assert.NotContains(t, out, "{{")
}

func TestRenderFailsRatherThanEmitHugoSyntax(t *testing.T) {
	_, err := render("---\ntitle: X\n---\n{{< grid >}}\n", composePage("x", "x"), "deadbeef", nil)

	require.Error(t, err)
}

// Every destination must be unique, or one page silently overwrites another.
func TestBundlePlanHasNoDuplicateOutputs(t *testing.T) {
	for _, b := range bundles {
		seen := set.New[string]()
		for _, p := range b.Pages {
			assert.False(t, seen.Contains(p.Out), "%s: %s written twice", b.Skill, p.Out)
			seen.Add(p.Out)
			assert.NotEqual(t, noticeFile, p.Out, "a page may not claim the notice filename")
		}
	}
}
