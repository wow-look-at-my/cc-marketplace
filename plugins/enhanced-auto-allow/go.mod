module enhanced-auto-allow

go 1.26

// The shell-reading vocabulary is shared with no-work-loss, in the repo rather
// than published: CI builds every plugin from a full checkout, and the released
// package carries the built binary, never this source.
replace shellwalk => ../../tools/shellwalk

require shellwalk v0.1.0

require (
	github.com/stretchr/testify v1.11.1
	github.com/wow-look-at-my/go-containers v0.0.0-20260826161058-40a3d1ef3d41 // go-toolchain:auto-branch
	mvdan.cc/sh/v3 v3.10.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
)

require gopkg.in/yaml.v3 v3.0.1 // indirect
