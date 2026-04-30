package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/wow-look-at-my/testify/assert"
	"github.com/wow-look-at-my/testify/require"
)

// Note: Schema validation is handled by plugin.bats during build

func TestGitStatusAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git status")
	assert.Equal(t, "allow", decision, "git status should be allowed")
}

func TestGitDiffAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git diff")
	assert.Equal(t, "allow", decision, "git diff should be allowed")
}

func TestGitLogAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log --oneline -10")
	assert.Equal(t, "allow", decision, "git log --oneline -10 should be allowed")
}

func TestGitBranchListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git branch -a")
	assert.Equal(t, "allow", decision, "git branch -a should be allowed")
}

func TestGitBranchDeletePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git branch -d feature")
	assert.Equal(t, "", decision, "git branch -d feature should passthrough")
}

func TestGitConfigGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config --get user.email")
	assert.Equal(t, "allow", decision, "git config --get user.email should be allowed")
}

func TestGitConfigListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config --list")
	assert.Equal(t, "allow", decision, "git config --list should be allowed")
}

func TestGitConfigSetPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config user.email foo@bar.com")
	assert.Equal(t, "", decision, "git config user.email foo@bar.com should passthrough")
}

func TestGitConfigLocalListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config --local --list 2>&1")
	assert.Equal(t, "allow", decision, "git config --local --list 2>&1 should be allowed")
}

func TestGitConfigGetUrlmatchAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git config --get-urlmatch http https://example.com")
	assert.Equal(t, "allow", decision, "git config --get-urlmatch should be allowed")
}

func TestGitReplaceListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git replace -l 2>&1")
	assert.Equal(t, "allow", decision, "git replace -l 2>&1 should be allowed")
}

func TestGitReplaceListLongAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git replace --list")
	assert.Equal(t, "allow", decision, "git replace --list should be allowed")
}

func TestGitReplaceWritePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git replace abc123 def456")
	assert.Equal(t, "", decision, "git replace (write) should passthrough")
}

func TestGitCommitGraphVerifyAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git commit-graph verify")
	assert.Equal(t, "allow", decision, "git commit-graph verify should be allowed")
}

func TestGitCommitGraphWritePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git commit-graph write")
	assert.Equal(t, "", decision, "git commit-graph write should passthrough")
}

func TestGitPushDryRunAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git push --dry-run origin main")
	assert.Equal(t, "allow", decision, "git push --dry-run should be allowed")
}

func TestGitPushDryRunShortAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git push -n origin main")
	assert.Equal(t, "allow", decision, "git push -n should be allowed")
}

func TestGitPushPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git push origin main")
	assert.Equal(t, "", decision, "git push without --dry-run should passthrough")
}

func TestStderrToStdoutRedirectAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git status 2>&1")
	assert.Equal(t, "allow", decision, "command with 2>&1 should be allowed")
}

func TestGitRemoteVerboseAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git remote -v")
	assert.Equal(t, "allow", decision, "git remote -v should be allowed")
}

func TestGitRemoteShowAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git remote show origin")
	assert.Equal(t, "allow", decision, "git remote show origin should be allowed")
}

func TestGitSubmoduleStatusAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git submodule status")
	assert.Equal(t, "allow", decision, "git submodule status should be allowed")
}

func TestGitSubmoduleStatusRecursiveAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git submodule status --recursive")
	assert.Equal(t, "allow", decision, "git submodule status --recursive should be allowed")
}

func TestGitSubmoduleUpdatePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git submodule update --init")
	assert.Equal(t, "", decision, "git submodule update --init should passthrough")
}

func TestGhPrListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh pr list")
	assert.Equal(t, "allow", decision, "gh pr list should be allowed")
}

func TestGhPrViewAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh pr view 123")
	assert.Equal(t, "allow", decision, "gh pr view 123 should be allowed")
}

func TestGhPrCreatePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh pr create")
	assert.Equal(t, "", decision, "gh pr create should passthrough")
}

func TestGhApiGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh api /repos/owner/repo")
	assert.Equal(t, "allow", decision, "gh api /repos/owner/repo should be allowed")
}

func TestGhApiPostPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh api -X POST /repos/owner/repo")
	assert.Equal(t, "", decision, "gh api -X POST should passthrough")
}

func TestGhBrowseAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh browse")
	assert.Equal(t, "allow", decision, "gh browse should be allowed")
}

func TestGhRunViewDenied(t *testing.T) {
	loadTestRules(t)
	decision, message := evaluateCommand("gh run view 123")
	assert.Equal(t, "deny", decision, "gh run view 123 should be denied")
	assert.NotEqual(t, "", message, "gh run view should have a deny message")
}

func TestGhRunWatchDenied(t *testing.T) {
	loadTestRules(t)
	decision, message := evaluateCommand("gh run watch 123")
	assert.Equal(t, "deny", decision, "gh run watch 123 should be denied")
	assert.NotEqual(t, "", message, "gh run watch should have a deny message")
}

func TestGhRunListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("gh run list")
	assert.Equal(t, "allow", decision, "gh run list should be allowed")
}

func TestFindPipeGrepAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`find /home/mhaynie -type f -name "*decode*" -o -name "*parse*" 2>/dev/null | grep -i tool`)
	assert.Equal(t, "allow", decision, "find|grep file search should be allowed")
}

func TestFindBasicAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("find . -name '*.go' -type f")
	assert.Equal(t, "allow", decision, "basic find should be allowed")
}

func TestFindExecGrepAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`find /home/mhaynie/repos/UnrealEngine -name "*.h" -type f -exec grep -l "class FSkeletalMeshSceneProxy" {} \;`)
	assert.Equal(t, "allow", decision, "find -exec grep should be allowed")
}

func TestFindExecRmPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("find . -name '*.tmp' -exec rm {} \\;")
	assert.Equal(t, "", decision, "find -exec rm should passthrough")
}

func TestFindDeletePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("find . -name '*.tmp' -delete")
	assert.Equal(t, "", decision, "find with -delete should passthrough")
}

func TestPkgConfigAllowed(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name    string
		command string
	}{
		{"cflags", "pkg-config --cflags openssl"},
		{"libs", "pkg-config --libs openssl"},
		{"modversion", "pkg-config --modversion openssl"},
		{"list-all", "pkg-config --list-all"},
		{"exists", "pkg-config --exists libcurl"},
		{"pkgconf alias", "pkgconf --cflags openssl"},
		{"pkg_config alias", "pkg_config --libs openssl"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, "allow", decision, "%q should be allowed", tt.command)
		})
	}
}

func TestLdconfigPrintCacheAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("ldconfig -p")
	assert.Equal(t, "allow", decision, "ldconfig -p should be allowed")
}

func TestLdconfigPrintCacheLongAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("ldconfig --print-cache")
	assert.Equal(t, "allow", decision, "ldconfig --print-cache should be allowed")
}

func TestLdconfigBarePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("ldconfig")
	assert.Equal(t, "", decision, "bare ldconfig should passthrough")
}

func TestGoVersionAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("go version")
	assert.Equal(t, "allow", decision, "go version should be allowed")
}

func TestGoEnvAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("go env GOPATH")
	assert.Equal(t, "allow", decision, "go env GOPATH should be allowed")
}

func TestGoDocAllowed(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name    string
		command string
	}{
		{"bare", "go doc fmt"},
		{"symbol", "go doc fmt.Println"},
		{"all flag", "go doc -all fmt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, "allow", decision, "%q should be allowed", tt.command)
		})
	}
}

func TestGoBuildPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("go build ./...")
	assert.Equal(t, "", decision, "go build should passthrough")
}

func TestWhichAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("which node")
	assert.Equal(t, "allow", decision, "which node should be allowed")
}

func TestWhichMultipleArgsAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("which git node python")
	assert.Equal(t, "allow", decision, "which git node python should be allowed")
}

func TestGrepAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("grep -ri 'TODO' src/")
	assert.Equal(t, "allow", decision, "grep should be allowed")
}

func TestCommandSubstitutionPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log $(echo test)")
	assert.Equal(t, "", decision, "command with substitution should passthrough")
}

func TestPipeBothAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log | head")
	assert.Equal(t, "allow", decision, "git log | head should be allowed")
}

func TestPipeOneUnknown(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("git log | python")
	assert.Equal(t, "", decision, "git log | python should passthrough")
}

func TestUnknownCommandPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("python --version")
	assert.Equal(t, "", decision, "unknown command python should passthrough")
}

func TestEchoRedirectPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("echo foo > file.txt")
	assert.Equal(t, "", decision, "echo with redirect should passthrough")
}

func TestEchoAppendPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("echo foo >> file.txt")
	assert.Equal(t, "", decision, "echo with append should passthrough")
}

func TestSortRedirectPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("sort input.txt > output.txt")
	assert.Equal(t, "", decision, "sort with redirect should passthrough")
}

func TestSafeRedirectAllowed(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"stdout to /tmp", "ls > /tmp/out.txt", "allow"},
		{"stdout append to /tmp", "ls >> /tmp/out.txt", "allow"},
		{"stdout to /dev/null", "ls > /dev/null", "allow"},
		{"stdout to nested /tmp", "ls > /tmp/sub/out.txt", "allow"},
		{"stderr to /tmp", "ls 2> /tmp/err.txt", "allow"},
		{"all to /tmp", "ls &> /tmp/out.txt", "allow"},
		{"all append to /tmp", "ls &>> /tmp/out.txt", "allow"},
		{"stdout to /etc passthrough", "ls > /etc/foo.txt", ""},
		{"stdout to relative passthrough", "ls > out.txt", ""},
		{"traversal out of /tmp passthrough", "ls > /tmp/../etc/passwd", ""},
		{"gh api to /tmp with stderr silenced and piped with wc", `gh api --hostname github.com -H "Accept: application/vnd.github.raw+json" "repos/hagezi/dns-blocklists/contents/wildcard/pro.mini-onlydomains.txt" 2>/dev/null > /tmp/hagezi.txt && wc -l /tmp/hagezi.txt`, "allow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, tt.expected, decision, "evaluateCommand(%q)", tt.command)
		})
	}
}

func TestEchoPipeAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("echo hello | grep hello")
	assert.Equal(t, "allow", decision, "echo piped to grep should be allowed")
}

func TestGitShowWithEchoAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`git show 54d7aa918a94 --stat && echo "---" && git show 54d7aa918a94 -- path/to/file.cpp`)
	assert.Equal(t, "allow", decision, "git show && echo && git show should be allowed")
}

func TestJustListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("just --list")
	assert.Equal(t, "allow", decision, "just --list should be allowed")
}

func TestJustListWithRedirectAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`just --list 2>/dev/null || echo "no justfile"`)
	assert.Equal(t, "allow", decision, "just --list 2>/dev/null || echo should be allowed")
}

func TestJustBuildPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("just build")
	assert.Equal(t, "", decision, "just build should passthrough")
}

func TestClaudeMcpListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp list")
	assert.Equal(t, "allow", decision, "claude mcp list should be allowed")
}

func TestClaudeMcpHelpAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp --help")
	assert.Equal(t, "allow", decision, "claude mcp --help should be allowed")
}

func TestClaudeMcpGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp get server-name")
	assert.Equal(t, "allow", decision, "claude mcp get server-name should be allowed")
}

func TestClaudeMcpAddPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp add my-server -- npx server")
	assert.Equal(t, "", decision, "claude mcp add should passthrough")
}

func TestClaudeMcpRemovePassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude mcp remove my-server")
	assert.Equal(t, "", decision, "claude mcp remove should passthrough")
}

func TestClaudeVersionAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude --version")
	assert.Equal(t, "allow", decision, "claude --version should be allowed")
}

func TestClaudeHelpAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude --help")
	assert.Equal(t, "allow", decision, "claude --help should be allowed")
}

func TestClaudePluginListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin list")
	assert.Equal(t, "allow", decision, "claude plugin list should be allowed")
}

func TestClaudePluginMarketplaceListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin marketplace list")
	assert.Equal(t, "allow", decision, "claude plugin marketplace list should be allowed")
}

func TestClaudePluginInstallPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin install some-plugin")
	assert.Equal(t, "", decision, "claude plugin install should passthrough")
}

func TestClaudeConfigListAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude config list")
	assert.Equal(t, "allow", decision, "claude config list should be allowed")
}

func TestClaudeConfigGetAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude config get key")
	assert.Equal(t, "allow", decision, "claude config get key should be allowed")
}

func TestClaudeConfigSetPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude config set key value")
	assert.Equal(t, "", decision, "claude config set should passthrough")
}

func TestClaudePluginHelpAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("claude plugin --help")
	assert.Equal(t, "allow", decision, "claude plugin --help should be allowed")
}

func TestClaudeHelpAlwaysAllowed(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"unknown subcommand with --help", "claude hook --help", "allow"},
		{"deep unknown subcommand with --help", "claude plugin marketplace add --help", "allow"},
		{"unknown subcommand with -h", "claude whatever -h", "allow"},
		{"unknown subcommand without help passthrough", "claude hook list", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, tt.expected, decision, "evaluateCommand(%q)", tt.command)
		})
	}
}

func TestDockerComposeRunRmAllowed(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"basic run --rm", "docker compose run --rm myservice", "allow"},
		{"run --rm with args", "docker compose run --rm myservice bash", "allow"},
		{"with -f flag", "docker compose -f docker-compose.yml run --rm myservice", "allow"},
		{"with --file flag", "docker compose --file docker-compose.yml run --rm myservice", "allow"},
		{"with -f and -p", "docker compose -f compose.yml -p myproject run --rm myservice", "allow"},
		{"--rm at end", "docker compose -f compose.yml run myservice --rm", "allow"},
		{"docker-compose alias", "docker-compose run --rm myservice", "allow"},
		{"docker-compose with -f", "docker-compose -f docker-compose.yml run --rm myservice", "allow"},
		{"run without --rm passthrough", "docker compose run myservice", ""},
		{"docker-compose run without --rm passthrough", "docker-compose run myservice", ""},
		{"compose up passthrough", "docker compose up", ""},
		{"compose down passthrough", "docker compose down", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, tt.expected, decision, "evaluateCommand(%q)", tt.command)
		})
	}
}

func TestMountBareAllowed(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("mount")
	assert.Equal(t, "allow", decision, "bare mount should be allowed")
}

func TestMountWithArgsPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("mount /dev/sda1 /mnt")
	assert.Equal(t, "", decision, "mount /dev/sda1 /mnt should passthrough")
}

func TestMountWithFlagsPassthrough(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("mount -t ext4 /dev/sda1 /mnt")
	assert.Equal(t, "", decision, "mount -t ext4 /dev/sda1 /mnt should passthrough")
}

func TestDockerComposePsAllowed(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"basic ps", "docker compose ps", "allow"},
		{"ps with flags", "docker compose ps --all", "allow"},
		{"ps with -f", "docker compose -f docker-compose.yml ps", "allow"},
		{"docker-compose ps", "docker-compose ps", "allow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, tt.expected, decision, "evaluateCommand(%q)", tt.command)
		})
	}
}

func TestDuplicateEntriesMerged(t *testing.T) {
	// Verify that when multiple nodes match the same command name,
	// their subcommands are effectively merged (allow wins over passthrough).
	saved := rules
	defer func() { rules = saved }()

	rules = Rules{
		Commands: []CommandNode{
			{
				Name:        "mycmd",
				Description: "first entry",
				Subcommands: []CommandNode{
					{Name: "sub1", AllowedFlags: "*"},
				},
			},
			{
				Name:        "mycmd",
				Description: "second entry",
				Subcommands: []CommandNode{
					{Name: "sub2", AllowedFlags: "*"},
				},
			},
		},
	}

	decision, _ := evaluateCommand("mycmd sub1")
	assert.Equal(t, "allow", decision, "mycmd sub1 should match first entry")

	decision, _ = evaluateCommand("mycmd sub2")
	assert.Equal(t, "allow", decision, "mycmd sub2 should match second entry")

	decision, _ = evaluateCommand("mycmd sub3")
	assert.Equal(t, "", decision, "mycmd sub3 should passthrough (no match)")
}

func TestDuplicateEntriesDenyWins(t *testing.T) {
	saved := rules
	defer func() { rules = saved }()

	rules = Rules{
		Commands: []CommandNode{
			{
				Name: "mycmd",
				Subcommands: []CommandNode{
					{Name: "ok", AllowedFlags: "*"},
				},
			},
			{
				Name: "mycmd",
				Subcommands: []CommandNode{
					{Name: "ok", DenyWithMessage: "blocked"},
				},
			},
		},
	}

	decision, msg := evaluateCommand("mycmd ok")
	assert.Equal(t, "deny", decision, "deny should win over allow for duplicate entries")
	assert.Equal(t, "blocked", msg)
}

func TestCompoundCommands(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name     string
		command  string
		expected string
	}{
		{"and both allowed", "git status && git diff", "allow"},
		{"and one unknown", "git status && python --version", ""},
		{"or both allowed", "git status || git diff", "allow"},
		{"fetch and show", "git fetch origin latest && git show HEAD", "allow"},
		{"basic commands", "ls && cat file.txt", "allow"},
		{"with deny", "git status && gh run view 123", "deny"},
		{"triple and", "git status && git diff && git log", "allow"},
		{"semicolon", "git status; git diff", "allow"},
		{"cd and git diff", "cd some/folder && git diff Config/DefaultEngine.ini", "allow"},
		{"cd and git log piped to grep -E", `cd /home/mhaynie/repos/UnrealEngine && git log --all --oneline | grep -E "e97a553|684663"`, "allow"},
		{"cd and git show --stat", "cd /home/mhaynie/repos/UnrealEngine && git show 60b658713bf7 --stat", "allow"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, tt.expected, decision, "evaluateCommand(%q)", tt.command)
		})
	}
}

func TestReadAllowed(t *testing.T) {
	input := `{"hook_event_name":"PermissionRequest","tool_name":"Read","tool_input":{"file_path":"/any/path/file.txt"}}`
	output := captureOutput(func() {
		old := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		go func() {
			w.Write([]byte(input))
			w.Close()
		}()
		main()
		os.Stdin = old
	})

	var resp PermissionResponse
	require.NoError(t, json.Unmarshal([]byte(output), &resp), "output was: %s", output)
	assert.Equal(t, "allow", resp.HookSpecificOutput.Decision.Behavior, "Read should be allowed")
}
