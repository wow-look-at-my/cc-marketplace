package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func skills(topics []topic) []string {
	out := make([]string, 0, len(topics))
	for _, t := range topics {
		out = append(out, t.Skill)
	}
	return out
}

func TestDockerfilePathsAreRecognised(t *testing.T) {
	for _, path := range []string{
		"Dockerfile",
		"/src/app/Dockerfile",
		"Dockerfile.dev",
		"dev.Dockerfile",
		"build/prod.dockerfile",
		"Containerfile",
		"Containerfile.test",
		"DOCKERFILE", // some trees shout
	} {
		assert.True(t, isDockerfilePath(path), "%s should be a Dockerfile", path)
	}
}

// The cost of a false positive is a reminder nobody needed, which is how a
// guard earns the reputation that gets it uninstalled.
func TestOrdinaryFilesAreNotDockerfiles(t *testing.T) {
	for _, path := range []string{
		"main.go",
		"README.md",
		"docs/dockerfile-tips.md",
		"dockerfiles.txt",
		"my-dockerfile-generator.py",
		"compose.yaml",
	} {
		assert.False(t, isDockerfilePath(path), "%s is not a Dockerfile", path)
	}
}

func TestComposePathsAreRecognised(t *testing.T) {
	for _, path := range []string{
		"compose.yaml",
		"compose.yml",
		"docker-compose.yml",
		"docker-compose.yaml",
		"deploy/compose.override.yaml",
		"docker-compose.prod.yml",
		"compose.test.override.yaml",
	} {
		assert.True(t, isComposePath(path), "%s should be a Compose file", path)
	}
}

// Most YAML is not Compose. Firing on all of it would make this hook noise.
func TestOtherYamlIsNotCompose(t *testing.T) {
	for _, path := range []string{
		"action.yml",
		".github/workflows/ci.yml",
		"config.yaml",
		"k8s/deployment.yaml",
		"compose.json",
		"mycompose.yaml",
		"Dockerfile",
	} {
		assert.False(t, isComposePath(path), "%s is not a Compose file", path)
	}
}

func TestEditingAFileNamesItsSkill(t *testing.T) {
	assert.Equal(t, []string{"docs:dockerfile"},
		skills(topicFor("Write", toolInput{FilePath: "Dockerfile"})))
	assert.Equal(t, []string{"docs:docker-compose"},
		skills(topicFor("Edit", toolInput{FilePath: "compose.yaml"})))
	assert.Empty(t, topicFor("Edit", toolInput{FilePath: "main.go"}))
}

// Reading one counts too: a question about a Dockerfile is answered from it,
// and answering from memory is the failure this exists to prevent.
func TestReadingAFileNamesItsSkill(t *testing.T) {
	assert.Equal(t, []string{"docs:dockerfile"},
		skills(topicFor("Read", toolInput{FilePath: "/repo/Dockerfile"})))
}

func TestBuildCommandsNameTheDockerfileSkill(t *testing.T) {
	for _, command := range []string{
		"docker build -t app .",
		"docker buildx build --platform linux/amd64 .",
		"cd src && docker build .",
	} {
		assert.Equal(t, []string{"docs:dockerfile"},
			skills(topicFor("Bash", toolInput{Command: command})), command)
	}
}

// The hyphenated spelling is Compose v1. Seeing it is itself a reason to load
// the skill, which is where the correction lives.
func TestComposeCommandsNameTheComposeSkill(t *testing.T) {
	for _, command := range []string{
		"docker compose up -d",
		"docker-compose up",
		"docker compose -f deploy/compose.yaml restart",
	} {
		assert.Equal(t, []string{"docs:docker-compose"},
			skills(topicFor("Bash", toolInput{Command: command})), command)
	}
}

// `docker compose build` genuinely touches both formats.
func TestACommandCanNameBothSkills(t *testing.T) {
	assert.Equal(t, []string{"docs:dockerfile", "docs:docker-compose"},
		skills(topicFor("Bash", toolInput{Command: "docker compose build && docker build ."})))
}

func TestUnrelatedCommandsNameNothing(t *testing.T) {
	for _, command := range []string{
		"go test ./...",
		"git commit -m 'docker build notes'", // the words, inside a message
		"grep -r 'docker compose' docs/",     // searching for them, not running them
		"echo docker",
		"npm run build",
		"rg 'docker build' README.md",
	} {
		assert.Empty(t, topicFor("Bash", toolInput{Command: command}), command)
	}
}

func TestAToolThatTouchesNoFileNamesNothing(t *testing.T) {
	assert.Empty(t, topicFor("Glob", toolInput{FilePath: "Dockerfile"}))
	assert.Empty(t, topicFor("Write", toolInput{}))
	assert.Empty(t, topicFor("Bash", toolInput{}))
}
