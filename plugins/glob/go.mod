module glob

go 1.26

// The MCP server and ripgrep runner are shared with the grep plugin, in the
// repo rather than published: CI builds every plugin from a full checkout.
replace rgmcp => ../../tools/rgmcp

require rgmcp v0.1.0

require (
	github.com/stretchr/testify v1.11.1
	github.com/wow-look-at-my/go-containers v0.0.0 // go-toolchain:auto-branch
	golang.org/x/text v0.40.0 // indirect
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
