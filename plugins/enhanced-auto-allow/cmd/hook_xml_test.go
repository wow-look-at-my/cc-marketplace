package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wow-look-at-my/testify/require"
)

func loadTestRulesJSON(t *testing.T) {
	t.Helper()
	repoRoot := getRepoRoot(t)
	rulesPath := filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.json")
	data, err := os.ReadFile(rulesPath)
	require.Nil(t, err, "Failed to read rules.json")
	require.NoError(t, json.Unmarshal(data, &rules), "Failed to parse rules.json")
}

func TestXMLMatchesJSON(t *testing.T) {
	repoRoot := getRepoRoot(t)

	jsonPath := filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.json")
	jsonData, err := os.ReadFile(jsonPath)
	require.NoError(t, err)
	var jsonRules Rules
	require.NoError(t, json.Unmarshal(jsonData, &jsonRules))

	xmlPath := filepath.Join(repoRoot, "plugins/enhanced-auto-allow/rules.xml")
	xmlData, err := os.ReadFile(xmlPath)
	require.NoError(t, err)
	xmlRules, err := loadXMLRules(xmlData)
	require.NoError(t, err)

	testCmds := []string{
		// simple readonly
		"ls", "ls -la", "cat foo.txt", "head -n 10 file", "tail -f log",
		"wc -l file", "stat file", "du -sh .", "df -h", "uname -a",
		"whoami", "id", "groups", "which node", "readlink -f foo",
		"echo hello", "printf '%s' foo", "true", "false", "test -f foo",
		"date", "hostname", "pwd", "env", "sort file", "uniq file",
		"tr a b", "cut -d: -f1 file", "diff a b", "seq 10", "realpath foo",
		"cd some/dir",
		// added readonly commands
		"dig example.com", "base64 file", "md5sum file", "sha256sum file",
		"xxd file", "jq . file.json", "nproc", "free -m", "lscpu", "lsblk",
		"fd pattern", "fdfind pattern", "readelf -h binary", "objdump -d binary",
		"nm binary", "size binary", "ldd binary", "rg pattern",
		// yq
		"yq '.key' file.yaml", "yq -r '.key' file.yaml",
		"yq -i '.key = 1' file.yaml", "yq --in-place '.key = 1' file.yaml",
		// mount
		"mount", "mount /dev/sda1 /mnt", "mount -t ext4 /dev/sda1 /mnt",
		// find
		"find . -name '*.go'", "find . -delete",
		`find . -name "*.h" -exec grep -l "foo" {} \;`,
		"find . -exec rm {} \\;",
		// grep
		"grep -ri pattern src/", "grep foo file",
		// awk
		"awk '{print $1}' file", "awk -F: '{print $1}' /etc/passwd",
		`awk 'BEGIN {system("rm -rf /")}'`, `awk '{getline line < "f"}'`,
		"gawk '{print}' file", "nawk '{print}' file", "mawk '{print}' file",
		"awk -f script.awk file", "awk --file=script.awk file",
		// ldconfig
		"ldconfig -p", "ldconfig --print-cache", "ldconfig",
		// go
		"go version", "go env GOPATH", "go doc fmt", "go build ./...",
		// git
		"git status", "git diff", "git log --oneline -10",
		"git branch -a", "git branch -d feature",
		"git config --get user.email", "git config --list",
		"git config user.email foo@bar.com",
		"git remote -v", "git remote show origin",
		"git submodule status", "git submodule update --init",
		"git replace -l", "git replace --list", "git replace abc def",
		"git commit-graph verify", "git commit-graph write",
		"git push --dry-run origin main", "git push -n origin main",
		"git push origin main",
		"git tag -l", "git tag --list",
		"git cat-file -t HEAD", "git for-each-ref",
		"git fetch origin latest",
		// gh
		"gh pr list", "gh pr view 123", "gh pr create",
		"gh issue list", "gh issue view 1",
		"gh api /repos/owner/repo", "gh api -X POST /repos/owner/repo",
		"gh api -X GET /repos", "gh api --method POST /repos",
		"gh browse", "gh run list", "gh run view 123", "gh run watch 123",
		"gh repo view owner/repo", "gh repo list",
		"gh release list", "gh release view v1",
		"gh workflow list", "gh workflow view ci",
		"gh search repos foo", "gh search code foo",
		"gh label list", "gh cache list",
		"gh gist list", "gh gist view abc",
		// claude
		"claude --version", "claude --help", "claude -h",
		"claude mcp list", "claude mcp get server", "claude mcp --help",
		"claude mcp add srv -- npx srv",
		"claude plugin list", "claude plugin marketplace list",
		"claude plugin install foo",
		"claude config list", "claude config get key", "claude config set k v",
		"claude hook --help", "claude whatever -h",
		// just
		"just --list", "just -l", "just --summary", "just build",
		// docker compose
		"docker compose run --rm svc", "docker compose run svc",
		"docker compose -f f.yml run --rm svc",
		"docker-compose run --rm svc", "docker-compose run svc",
		"docker compose ps", "docker compose up",
		"docker-compose ps", "docker-compose logs",
		// docker readonly
		"docker ps", "docker images", "docker logs ctr", "docker version",
		"docker image ls", "docker container ls", "docker network ls",
		"docker volume ls", "docker system info", "docker buildx ls",
		// zpool/zfs
		"zpool list", "zpool status", "zfs list", "zfs get all",
		// apt/dpkg
		"apt list", "apt show pkg", "apt search foo",
		"apt-cache search foo", "apt-cache policy pkg",
		"apt-mark showmanual",
		"dpkg -l", "dpkg --list", "dpkg -L pkg", "dpkg -s pkg",
		"dpkg", "dpkg --configure -a",
		"dpkg-query -W", "dpkg-query -l",
		// brew
		"brew list", "brew info pkg", "brew search foo",
		"brew tap", "brew tap homebrew/cask",
		"brew install foo",
		// compound
		"git status && git diff", "git status && python foo",
		"git log | head", "ls | python",
		// redirect
		"echo foo > file.txt", "ls > /dev/null", "ls > /tmp/out.txt",
		"git status 2>&1",
		// dangerous
		"rm -rf /", "python script.py",
	}

	var mismatches []string
	for _, cmd := range testCmds {
		rules = jsonRules
		jDec, jMsg := evaluateCommand(cmd)

		rules = xmlRules
		xDec, xMsg := evaluateCommand(cmd)

		if jDec != xDec || jMsg != xMsg {
			mismatches = append(mismatches, cmd+
				" -> JSON("+jDec+","+jMsg+") XML("+xDec+","+xMsg+")")
		}
	}

	for _, m := range mismatches {
		t.Errorf("MISMATCH: %s", m)
	}
	if len(mismatches) == 0 {
		t.Logf("All %d commands matched between JSON and XML", len(testCmds))
	}
}
