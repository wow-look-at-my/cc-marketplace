package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routeCase is one write route: the command that must be refused, the control
// that must still run, and the text the refusal has to name.
type routeCase struct {
	route string
	deny  string
	allow string
	names string
	setup func(t *testing.T, root, out string)
}

// routeCases covers every route the rule closes. {{tree}} is the working tree
// and {{out}} a directory outside it, so the deny and the control differ only in
// where the write lands.
func routeCases() []routeCase {
	return []routeCase{
		// In-place editors.
		{route: "sed -i", deny: "sed -i s/a/b/ src.txt", allow: "sed -i s/a/b/ {{out}}/src.txt", names: "src.txt"},
		{route: "sed -i with a suffix in a flag cluster", deny: "sed -ri.bak -e s/a/b/ notes.md", allow: "sed -ri.bak -e s/a/b/ {{out}}/src.txt", names: "notes.md"},
		{route: "sed w command", deny: `sed -n 's/a/b/w out.txt' src.txt`, allow: `sed -n 's/a/b/w {{out}}/out.txt' src.txt`, names: "out.txt"},
		{route: "sed -i with files supplied by xargs", deny: "ls | xargs sed -i s/a/b/", allow: "ls | xargs sed s/a/b/", names: "supplied at runtime"},
		{route: "ed", deny: "ed -s src.txt", allow: "ed -s {{out}}/src.txt", names: "src.txt"},
		{route: "ex", deny: "ex -s -c wq src.txt", allow: "ex -s -c wq {{out}}/src.txt", names: "src.txt"},
		{route: "vi -c", deny: "vi -c :wq src.txt", allow: "vi -c :wq {{out}}/src.txt", names: "src.txt"},
		{route: "emacs --batch --eval on a named file", deny: `emacs --batch --eval '(insert "x")' src.txt`, allow: `emacs --batch --eval '(insert "x")' {{out}}/src.txt`, names: "src.txt"},
		{route: "emacs --batch --eval with the file named in the script", deny: `emacs --batch --eval '(write-region "x" nil "src.txt")'`, allow: "emacs --version", names: "named in its script"},
		{route: "awk -i inplace", deny: `gawk -i inplace '{print}' src.txt`, allow: `gawk -i inplace '{print}' {{out}}/src.txt`, names: "src.txt"},
		{route: "awk print redirect", deny: `awk '{print > "out.txt"}' src.txt`, allow: `awk '{print > "{{out}}/out.txt"}' src.txt`, names: "out.txt"},
		{route: "ruby -pi -e", deny: `ruby -pi -e 'gsub(/a/,"b")' src.txt`, allow: "ruby --version", names: "inline ruby script"},
		{route: "node -e with fs.writeFileSync", deny: `node -e 'require("fs").writeFileSync("src.txt","x")'`, allow: "node --version", names: "inline node script"},
		{route: "perl -pi -e", deny: `perl -pi -e 's/a/b/' src.txt`, allow: "perl --version", names: "inline perl script"},
		// The control is the shape this used to refuse: an interpreter that already
		// names a script is being fed INPUT, and the program it runs is in the
		// command text. Denying it stops a hook being tested with a real payload.
		{route: "a script piped into an interpreter", deny: `echo 'x' | ruby`, allow: `echo 'x' | ruby prog.rb`, names: "piped in on stdin"},
		{route: "a script redirected into an interpreter", deny: `ruby < prog.rb`, allow: `ruby prog.rb < data.json`, names: "piped in on stdin"},
		{route: "busybox sed -i", deny: "busybox sed -i s/a/b/ src.txt", allow: "busybox sed -i s/a/b/ {{out}}/src.txt", names: "src.txt"},
		{route: "sponge", deny: "sort src.txt | sponge src.txt", allow: "sort src.txt | sponge {{out}}/src.txt", names: "src.txt"},
		{route: "xxd -r", deny: "xxd -r -p {{out}}/dump.hex src.txt", allow: "xxd -r -p {{out}}/dump.hex {{out}}/src.txt", names: "src.txt"},
		{route: "base64 -d into a redirect", deny: "base64 -d {{out}}/in.b64 > src.txt", allow: "base64 -d {{out}}/in.b64 > {{out}}/src.txt", names: "src.txt"},

		// Redirection and copy-over.
		{route: "> redirect", deny: "echo hi > src.txt", allow: "echo hi > {{out}}/src.txt", names: "src.txt"},
		{route: ">> redirect", deny: "echo hi >> src.txt", allow: "echo hi >> {{out}}/src.txt", names: "src.txt"},
		{route: ">| redirect", deny: "echo hi >| src.txt", allow: "echo hi >| {{out}}/src.txt", names: "src.txt"},
		{route: "redirect off a brace block", deny: "{ echo hi; echo there; } > src.txt", allow: "{ echo hi; echo there; } > {{out}}/src.txt", names: "src.txt"},
		{route: "tee", deny: "echo hi | tee src.txt", allow: "echo hi | tee {{out}}/src.txt", names: "src.txt"},
		{route: "tee -a", deny: "echo hi | tee -a src.txt", allow: "echo hi | tee -a {{out}}/src.txt", names: "src.txt"},
		{route: "dd of=", deny: "dd if=/dev/zero of=src.txt", allow: "dd if=/dev/zero of={{out}}/src.txt", names: "src.txt"},
		{route: "truncate", deny: "truncate -s 0 src.txt", allow: "truncate -s 0 {{out}}/src.txt", names: "src.txt"},
		{route: "cp over a tracked file", deny: "cp {{out}}/src.txt src.txt", allow: "cp src.txt {{out}}/copy.txt", names: "src.txt"},
		{route: "mv over a tracked file", deny: "mv {{out}}/staged.txt src.txt", allow: "mv src.txt {{out}}/staged.txt", names: "src.txt"},
		{route: "install", deny: "install -m 644 {{out}}/src.txt src.txt", allow: "install -m 644 src.txt {{out}}/copy.txt", names: "src.txt"},
		{route: "rsync into the tree", deny: "rsync -a {{out}}/src.txt src.txt", allow: "rsync -a src.txt {{out}}/copy.txt", names: "src.txt"},

		// Write elsewhere, then splice the fragment in.
		{route: "sed -i r, reading a fragment written elsewhere", deny: "sed -i '3r {{out}}/frag.txt' src.txt", allow: "sed -i '3r {{out}}/frag.txt' {{out}}/src.txt", names: "src.txt"},
		{route: "appending a fragment written elsewhere", deny: "cat {{out}}/frag.txt >> src.txt", allow: "cat src.txt >> {{out}}/frag.txt", names: "src.txt"},

		// Patch application.
		{route: "patch", deny: "patch -p1 < {{out}}/x.diff", allow: "cd {{out}} && patch -p1 < x.diff", names: "patch"},
		{route: "git apply", deny: "git apply {{out}}/x.diff", allow: "cd {{out}} && git apply x.diff", names: "git apply"},
		{route: "git apply --cached", deny: "git apply --cached {{out}}/x.diff", allow: "cd {{out}} && git apply --cached x.diff", names: "git apply"},
		{route: "git am", deny: "git am {{out}}/x.patch", allow: "cd {{out}} && git am x.patch", names: "git am"},

		// git used as an editor.
		{route: "git checkout with a pathspec", deny: "git checkout master -- src", allow: "cd {{out}} && git checkout master -- src", names: "git checkout"},
		{route: "git restore", deny: "git restore src.txt", allow: "cd {{out}} && git restore src.txt", names: "git restore"},
		{route: "git stash pop", deny: "git stash pop", allow: "cd {{out}} && git stash pop", names: "git stash pop"},
		{route: "git revert", deny: "git revert HEAD", allow: "cd {{out}} && git revert HEAD", names: "git revert"},
		{route: "git cherry-pick", deny: "git cherry-pick abc123", allow: "cd {{out}} && git cherry-pick abc123", names: "git cherry-pick"},
		{route: "git merge", deny: "git merge origin/master", allow: "cd {{out}} && git merge origin/master", names: "git merge"},
		{route: "git reset --hard", deny: "git reset --hard origin/master", allow: "cd {{out}} && git reset --hard origin/master", names: "git reset"},

		// The plumbing route, which never touches the worktree at all.
		{route: "git hash-object -w", deny: "git hash-object -w {{out}}/blob.txt", allow: "cd {{out}} && git hash-object -w blob.txt", names: "git hash-object"},
		{route: "git update-index --cacheinfo", deny: "git update-index --cacheinfo 100644,abc123,src.txt", allow: "cd {{out}} && git update-index --cacheinfo 100644,abc123,src.txt", names: "git update-index"},
		{route: "git commit-tree", deny: "git commit-tree abc123 -m x", allow: "cd {{out}} && git commit-tree abc123 -m x", names: "git commit-tree"},
		{route: "git update-ref", deny: "git update-ref refs/heads/x abc123", allow: "cd {{out}} && git update-ref refs/heads/x abc123", names: "git update-ref"},

		// Extraction and download into the tree.
		{route: "tar -x", deny: "tar -xzf {{out}}/a.tgz", allow: "tar -xzf {{out}}/a.tgz -C {{out}}", names: "tar -x"},
		{route: "unzip", deny: "unzip -o {{out}}/a.zip", allow: "unzip -o {{out}}/a.zip -d {{out}}", names: "unzip"},
		{route: "curl -o", deny: "curl -sL -o src.txt https://example.com/x", allow: "curl -sL -o {{out}}/src.txt https://example.com/x", names: "src.txt"},
		{route: "curl -O", deny: "curl -sLO https://example.com/x.tgz", allow: "curl -sLO --output-dir {{out}} https://example.com/x.tgz", names: "curl -O"},
		{route: "wget -O", deny: "wget -O src.txt https://example.com/x", allow: "wget -O {{out}}/src.txt https://example.com/x", names: "src.txt"},
		{route: "gh release download", deny: "gh release download v1 -R o/r", allow: "gh release download v1 -R o/r -D {{out}}", names: "gh release download"},
		{route: "curl piped into tar", deny: "curl -sL https://example.com/x.tgz | tar -xz", allow: "curl -sL https://example.com/x.tgz | tar -xz -C {{out}}", names: "tar -x"},

		// Indirection.
		{
			route: "a shell script that writes",
			deny:  "bash ./writer.sh",
			allow: "bash ./reader.sh",
			names: "src.txt",
			setup: func(t *testing.T, root, out string) {
				writeFile(t, filepath.Join(root, "writer.sh"), "#!/bin/bash\necho hi > src.txt\n")
				writeFile(t, filepath.Join(root, "reader.sh"), "#!/bin/bash\ngrep hi src.txt\n")
			},
		},
		{
			route: "a script run by its own shebang",
			deny:  "./shebang.sh",
			allow: "./reader.sh",
			names: "src.txt",
			setup: func(t *testing.T, root, out string) {
				writeFile(t, filepath.Join(root, "shebang.sh"), "#!/bin/sh\nsed -i s/a/b/ src.txt\n")
				writeFile(t, filepath.Join(root, "reader.sh"), "#!/bin/bash\ngrep hi src.txt\n")
			},
		},
		{
			route: "a script this hook cannot parse",
			deny:  "bash ./broken.sh",
			allow: "bash ./reader.sh",
			names: "does not parse as shell",
			setup: func(t *testing.T, root, out string) {
				writeFile(t, filepath.Join(root, "broken.sh"), "#!/bin/bash\necho 'unterminated\n")
				writeFile(t, filepath.Join(root, "reader.sh"), "#!/bin/bash\ngrep hi src.txt\n")
			},
		},
		{
			route: "an ad-hoc script under a temporary directory",
			deny:  "node {{out}}/gen.js",
			allow: "node scripts/build.js",
			names: "temporary directory",
			setup: func(t *testing.T, root, out string) {
				writeFile(t, filepath.Join(root, "scripts", "build.js"), "// project tooling\n")
				writeFile(t, filepath.Join(out, "gen.js"), "// scratch\n")
			},
		},
		{route: "find -exec", deny: `find . -name '*.txt' -exec sed -i s/a/b/ {} +`, allow: `cd {{out}} && find . -name '*.txt' -exec sed -i s/a/b/ {} +`, names: "sed -i"},
		{route: "a writer started in the background", deny: "sed -i s/a/b/ src.txt &", allow: "sed -i s/a/b/ {{out}}/src.txt &", names: "src.txt"},
		{route: "a shell function wrapping a writer", deny: "f() { sed -i s/a/b/ src.txt; }; f", allow: "f() { sed -i s/a/b/ {{out}}/src.txt; }; f", names: "src.txt"},
		{route: "an alias wrapping a writer", deny: "alias fix='sed -i s/a/b/ src.txt'; fix", allow: "alias fix='sed -i s/a/b/ {{out}}/src.txt'; fix", names: "src.txt"},
		{route: "sh -c", deny: `sh -c 'echo hi > src.txt'`, allow: `sh -c 'echo hi > {{out}}/src.txt'`, names: "src.txt"},

		// Symlinks.
		{route: "ln -sf over a tracked path", deny: "ln -sf {{out}}/src.txt src.txt", allow: "ln -sf src.txt {{out}}/link.txt", names: "src.txt"},

		// Writes that never touch a local file.
		{route: "gh api PUT on contents", deny: "gh api --method PUT /repos/o/r/contents/README.md -f content=abc", allow: "gh api /repos/o/r/contents/README.md", names: "GitHub API"},
		{route: "gh api createCommitOnBranch", deny: `gh api graphql -f query='mutation { createCommitOnBranch(input: $i) { commit { url } } }'`, allow: `gh api graphql -f query='query { viewer { login } }'`, names: "GitHub API"},
		{route: "curl PUT on contents", deny: "curl -X PUT https://api.github.com/repos/o/r/contents/README.md -d @{{out}}/body.json", allow: "curl https://api.github.com/repos/o/r/contents/README.md", names: "GitHub API"},

		// Ambiguity, which fails closed.
		{route: "a command that does not parse", deny: "echo 'unfinished", allow: "echo fine", names: "does not parse as shell"},
		{route: "a target built from an expansion", deny: `sed -i s/a/b/ "$TARGET"`, allow: "sed -i s/a/b/ {{out}}/src.txt", names: "expansion"},
		{route: "a cd this hook cannot follow", deny: `cd "$D" && echo hi > f.txt`, allow: "cd {{out}} && echo hi > f.txt", names: "not statically known"},
		{route: "a git repository relocated by the environment", deny: "GIT_DIR={{out}}/.git git apply x.diff", allow: "cd {{out}} && git apply x.diff", names: "relocated"},

		// Re-granting what the guard denies.
		{route: "a redirect into the live settings", deny: "echo '{}' > ~/.claude/settings.json", allow: "echo '{}' > {{out}}/settings.json", names: "live Claude Code settings"},

		// Found by auditing the commands this environment's permission rules
		// already allow, rather than by listing routes from memory.
		{route: "sort -o", deny: "sort -o src.txt src.txt", allow: "sort -o {{out}}/src.txt src.txt", names: "src.txt"},
		{route: "split", deny: "split -l 100 {{out}}/big.txt part-", allow: "split -l 100 {{out}}/big.txt {{out}}/part-", names: "split"},
		{route: "gzip replacing a tracked file", deny: "gzip notes.md", allow: "gzip {{out}}/notes.md", names: "notes.md"},
		{route: "zip creating an archive in the tree", deny: "zip bundle.zip src.txt", allow: "zip {{out}}/bundle.zip src.txt", names: "bundle.zip"},
		{route: "docker cp out of a container", deny: "docker cp web:/etc/nginx.conf src.txt", allow: "docker cp web:/etc/nginx.conf {{out}}/src.txt", names: "src.txt"},
		{route: "scp from a remote host", deny: "scp host:/etc/hosts src.txt", allow: "scp src.txt host:/tmp/hosts", names: "src.txt"},
		{route: "yq -i", deny: "yq -i '.a = 1' config.yaml", allow: "yq -i '.a = 1' {{out}}/config.yaml", names: "config.yaml"},

		// A formatter this hook does not vouch for.
		{route: "an unrecognised in-place rewriter", deny: "ffs fmt -w builtins/math.ffs", allow: "ffs check builtins/math.ffs", names: "builtins/math.ffs"},
	}
}

func TestEveryRouteIsDeniedInsideTheTree(t *testing.T) {
	for _, c := range routeCases() {
		t.Run(c.route, func(t *testing.T) {
			root, out := newTree(t), outsideTree(t)
			if c.setup != nil {
				c.setup(t, root, out)
			}
			cmd := fill(c.deny, root, out)
			reason := ask(t, root, cmd)
			require.NotEmpty(t, reason, "%s must be denied: %s", c.route, cmd)
			assert.Contains(t, reason, c.names,
				"the denial must name what it stopped, or an unrelated rule could satisfy this test")
			assert.True(t,
				strings.Contains(reason, "Use Edit") || strings.Contains(reason, "ask the user") || strings.Contains(reason, "run: "),
				"the denial must say what to do instead, or the reader goes hunting for a way around it: %s", reason)
		})
	}
}

// The control is what makes the deny case load-bearing: the same command, the
// same shape, aimed somewhere this hook does not protect.
func TestEveryRouteIsAllowedOutsideTheTree(t *testing.T) {
	for _, c := range routeCases() {
		t.Run(c.route, func(t *testing.T) {
			root, out := newTree(t), outsideTree(t)
			if c.setup != nil {
				c.setup(t, root, out)
			}
			cmd := fill(c.allow, root, out)
			assert.Empty(t, ask(t, root, cmd), "%s must still run outside the tree: %s", c.route, cmd)
		})
	}
}

// Removing this plugin makes every deny case allowed, so the assertion above is
// the one that turns red. Asserting that here keeps the claim honest rather than
// leaving it to be believed: nothing else in the suite would notice a hook that
// stopped deciding.
func TestDenyAssertionsFailWithNoHookInPlace(t *testing.T) {
	noHook := func(string, string) string { return "" }
	for _, c := range routeCases() {
		root, out := newTree(t), outsideTree(t)
		require.Empty(t, noHook(root, fill(c.deny, root, out)),
			"with no hook every command is allowed, which is what TestEveryRouteIsDeniedInsideTheTree catches")
	}
}

// Ordinary work must not trip any of this. A guard that denies the commands a
// session runs all day gets uninstalled, and then it protects nothing.
func TestOrdinaryCommandsAreUntouched(t *testing.T) {
	allowed := []string{
		// git, which Bash exists to run.
		"git status", "git add .", `git commit -m "message"`, "git push -u origin HEAD",
		"git checkout -b claude/fix-thing", "git switch -c claude/other", "git stash push -u",
		"git restore --staged src.txt", "git reset src.txt", "git diff", "git log --oneline -5",
		"git fetch origin master", "git branch -a", "git clone https://example.com/r.git {{out}}/r",

		// Builds, tests and search.
		"go build ./...", "go test ./...", "npm test", "make build", "just fmt",
		"grep -w foo src.txt", "rg --files", "ls -la", "find . -name '*.go'",
		`awk '$1 > 5' src.txt`, `awk '{print $2}' src.txt`, "sed s/a/b/ src.txt",
		"sort src.txt | uniq -c", "jq '.name' package.json", "echo hi > /dev/null",
		"gzip -c notes.md", "docker compose up -d", "docker cp src.txt web:/tmp/x",
		"yq '.a' config.yaml", "sort -k 2 src.txt",
		"go test ./... 2>&1", "diff src.txt notes.md",

		// The formatters this hook vouches for, and the recipe route.
		"gofmt -w .", "goimports -w src", "go-toolchain", "go generate ./...",
		"prettier --write src", "shfmt -w script.sh", "cargo fmt",

		// Paths a build owns, and everything outside the tree.
		"echo hi > build/app.js", "sed -i s/a/b/ node_modules/dep.js",
		"tar -xzf {{out}}/a.tgz -C build", "echo hi > {{out}}/scratch.txt",

		// Reads of the very things the write rules cover.
		"gh api /repos/o/r/contents/README.md", "gh pr view 1", "gh run list",
	}
	for _, cmd := range allowed {
		t.Run(strings.ReplaceAll(cmd, "/", "_"), func(t *testing.T) {
			root, out := newTree(t), outsideTree(t)
			writeFile(t, filepath.Join(root, "script.sh"), "#!/bin/bash\necho hi\n")
			assert.Empty(t, ask(t, root, fill(cmd, root, out)), "ordinary command must run: %s", cmd)
		})
	}
}
