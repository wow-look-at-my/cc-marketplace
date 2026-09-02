package main

import (
	"path/filepath"
	"regexp"
	"strings"
)

// topic is one docs skill this hook can point the model at.
type topic struct {
	Skill string // the slash command, e.g. "docs:dockerfile"
	What  string // what the target is, for the message
	Read  string // the reference file to name first
}

var (
	dockerfileTopic = topic{
		Skill: "docs:dockerfile",
		What:  "a Dockerfile",
		Read:  "reference/dockerfile.md",
	}
	composeTopic = topic{
		Skill: "docs:docker-compose",
		What:  "a Compose file",
		Read:  "reference/services.md",
	}
)

// A Dockerfile is identified by name, because it has no extension of its own in
// its usual spelling. All four shapes below are conventional: the bare name, a
// staged variant (`Dockerfile.dev`), a prefixed one (`dev.Dockerfile`), and the
// Podman spelling.
func isDockerfilePath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case name == "dockerfile" || name == "containerfile":
		return true
	case strings.HasPrefix(name, "dockerfile.") || strings.HasPrefix(name, "containerfile."):
		return true
	case strings.HasSuffix(name, ".dockerfile") || strings.HasSuffix(name, ".containerfile"):
		return true
	}
	return false
}

// composeName matches the four supported base names plus the override and
// profile variants (`compose.override.yaml`, `docker-compose.prod.yml`).
//
// It deliberately does NOT match every YAML file. A hook that fires on all of
// them is one the reader learns to skim past, and most YAML is not Compose.
var composeName = regexp.MustCompile(`^(docker-)?compose(\.[a-z0-9_-]+)*\.ya?ml$`)

func isComposePath(path string) bool {
	return composeName.MatchString(strings.ToLower(filepath.Base(path)))
}

// buildCommand and composeCommand find the two command families in a shell
// string. `docker-compose` (v1, hyphenated) counts: seeing it is itself a
// reason to load the skill, which explains why it is the wrong command.
var (
	buildCommand   = regexp.MustCompile(`\bdocker\s+(buildx\s+)?build\b`)
	composeCommand = regexp.MustCompile(`\bdocker(\s+|-)compose\b`)
)

// topicFor decides which skill, if any, a tool call should pull in.
//
// A call can legitimately touch both -- `docker compose build` reads a Compose
// file and builds a Dockerfile -- so this returns every topic that applies,
// in a stable order.
func topicFor(tool string, input toolInput) []topic {
	switch tool {
	case "Read", "Write", "Edit", "MultiEdit", "NotebookEdit":
		return pathTopics(input.FilePath)
	case "Bash":
		return commandTopics(input.Command)
	}
	return nil
}

func pathTopics(path string) []topic {
	if path == "" {
		return nil
	}
	var out []topic
	if isDockerfilePath(path) {
		out = append(out, dockerfileTopic)
	}
	if isComposePath(path) {
		out = append(out, composeTopic)
	}
	return out
}

func commandTopics(command string) []topic {
	if command == "" {
		return nil
	}
	lower := strings.ToLower(command)

	var out []topic
	if buildCommand.MatchString(lower) {
		out = append(out, dockerfileTopic)
	}
	if composeCommand.MatchString(lower) {
		out = append(out, composeTopic)
	}
	return out
}
