package main

import (
	"testing"

	"github.com/wow-look-at-my/testify/assert"
)

func TestAwkAllowed(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name    string
		command string
	}{
		{"print column", "awk '{print $1}' file.txt"},
		{"field separator", "awk -F: '{print $1}' /etc/passwd"},
		{"long field separator", "awk --field-separator=: '{print $1}' /etc/passwd"},
		{"nr comparison", "awk 'NR==5' file.txt"},
		{"pattern match", "awk '/pattern/' file.txt"},
		{"sum with END", "awk '{sum+=$1} END {print sum}' file.txt"},
		{"variable assign", "awk -v x=5 '{print $x}' file.txt"},
		{"long variable assign", "awk --assign=x=5 '{print $x}' file.txt"},
		{"multi-line script", `awk 'BEGIN {FS=":"} {print $1}' /etc/passwd`},
		{"length comparison", "awk 'length > 100'"},
		{"logical or", "awk '$1 || $2' file"},
		{"regex alternation", "awk '/foo|bar/' file"},
		{"count occurrences", "awk '{count[$1]++} END {for (k in count) print k, count[k]}' file"},
		{"piped input", "cat file.txt | awk '{print $1}'"},
		{"gawk alias", "gawk '{print $1}' file.txt"},
		{"nawk alias", "nawk '{print NR, $0}' file.txt"},
		{"mawk alias", "mawk '{print $NF}' file.txt"},
		{"source flag", "awk -e '{print}' file.txt"},
		{"inline arithmetic", "awk '{print $1 * 2}' file.txt"},
		{"printf", `awk '{printf "%s\n", $1}' file`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, "allow", decision, "evaluateCommand(%q)", tt.command)
		})
	}
}

func TestAwkDangerousPassthrough(t *testing.T) {
	loadTestRules(t)
	tests := []struct {
		name    string
		command string
	}{
		{"system call", `awk 'BEGIN {system("rm -rf /")}'`},
		{"system with space", `awk 'BEGIN {system ("ls")}'`},
		{"getline", `awk '{getline line < "/etc/passwd"; print line}'`},
		{"getline from command", `awk 'BEGIN {"ls" | getline x; print x}'`},
		{"redirect to quoted file", `awk '{print > "/tmp/out.txt"}' file`},
		{"redirect no space", `awk '{print >"/tmp/out.txt"}' file`},
		{"append redirect", `awk '{print >> "log.txt"}' file`},
		{"pipe to command", `awk '{print | "wc -l"}' file`},
		{"pipe no space", `awk '{print|"wc -l"}' file`},
		{"script file flag", "awk -f /tmp/script.awk data.txt"},
		{"long script file flag", "awk --file=/tmp/script.awk data.txt"},
		{"gawk include", "gawk '@include \"lib.awk\"; {print}' file"},
		{"gawk load", "gawk '@load \"extension\"'"},
		{"gawk include flag", "gawk -i library.awk '{print}' file"},
		{"gawk load flag", "gawk -l extension '{print}' file"},
		{"gawk execute", "gawk -E /tmp/script.awk data"},
		{"gawk profile", "gawk -p '{print}' file"},
		{"gawk pretty-print", "gawk -o '{print}' file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, _ := evaluateCommand(tt.command)
			assert.Equal(t, "", decision, "evaluateCommand(%q) should passthrough", tt.command)
		})
	}
}

func TestAwkPipeChain(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand("cat /etc/hosts | awk '{print $2}' | sort -u")
	assert.Equal(t, "allow", decision, "cat|awk|sort pipeline should be allowed")
}

func TestAwkPipeChainWithSystem(t *testing.T) {
	loadTestRules(t)
	decision, _ := evaluateCommand(`cat /etc/hosts | awk 'BEGIN{system("id")}'`)
	assert.Equal(t, "", decision, "pipeline with awk using system() should passthrough")
}
