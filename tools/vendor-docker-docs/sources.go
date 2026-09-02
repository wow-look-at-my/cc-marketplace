package main

// upstream is one repository the reference text is vendored from. Ref is the
// branch resolved to a commit at run time, so a regenerate records what it read.
type upstream struct {
	Repo    string
	Ref     string
	Blob    string // URL template for a human-readable link, %s is the commit
	License string
}

var (
	dockerDocs = upstream{
		Repo:    "docker/docs",
		Ref:     "main",
		Blob:    "https://github.com/docker/docs/blob/%s/%s",
		License: "Apache-2.0",
	}
	buildkit = upstream{
		Repo:    "moby/buildkit",
		Ref:     "master",
		Blob:    "https://github.com/moby/buildkit/blob/%s/%s",
		License: "Apache-2.0",
	}
)

// page is one vendored file: an upstream path, and the name it takes on disk
// under the destination skill's reference directory.
type page struct {
	Src  upstream
	Path string
	Out  string
}

// bundle is every page vendored into one skill.
type bundle struct {
	Skill string
	Pages []page
}

func composePage(name, out string) page {
	return page{Src: dockerDocs, Path: "content/reference/compose-file/" + name + ".md", Out: out + ".md"}
}

// bundles is the whole vendoring plan. Adding a page here is the only edit a
// new reference file needs.
//
// The Compose landing page (_index.md) is deliberately absent: it is a
// navigation grid with no specification text in it.
var bundles = []bundle{
	{
		Skill: "dockerfile",
		Pages: []page{{
			Src:  buildkit,
			Path: "frontend/dockerfile/docs/reference.md",
			Out:  "dockerfile.md",
		}},
	},
	{
		Skill: "docker-compose",
		Pages: []page{
			composePage("services", "services"),
			composePage("build", "build"),
			composePage("deploy", "deploy"),
			composePage("develop", "develop"),
			composePage("networks", "networks"),
			composePage("volumes", "volumes"),
			composePage("configs", "configs"),
			composePage("secrets", "secrets"),
			composePage("models", "models"),
			composePage("profiles", "profiles"),
			composePage("include", "include"),
			composePage("merge", "merge"),
			composePage("interpolation", "interpolation"),
			composePage("fragments", "fragments"),
			composePage("extension", "extension"),
			composePage("version-and-name", "version-and-name"),
		},
	},
}

// includeRoot is where a Hugo `{{% include "x.md" %}}` resolves from. Only
// docker/docs uses them.
const includeRoot = "content/includes/"
