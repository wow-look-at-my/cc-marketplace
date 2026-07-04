# cleanup-bash-cmds AST transform (driven by hook.sh).
#
# Input: a shfmt --to-json syntax tree (mvdan.cc/sh typed JSON).
# $ops:  operator token numbers probed at runtime from the SAME shfmt binary
#        ({gt, app, dup, and, or, pipe, pipeall, hdoc, dashhdoc}). The
#        numeric values differ between shfmt versions (e.g. "|" is 12 in
#        v3.8.0 but 13 in v3.13.1), so they must never be hardcoded.
# Output: {deny, changed, silent, message, ast}
#   deny:    command contains a heredoc; block it (nothing else applies)
#   changed: the tree semantically changed (a rewrite should be emitted)
#   silent:  changed, but only silent-class rules fired (pipefail injection,
#            trailing-2>&1 removal) -- no user-facing announcement
#   message: "; "-joined clauses for the LOUD rules that fired, each rendered
#            from the actual removed/rewritten node, e.g.
#            "removed 2>/dev/null", "removed | head -50",
#            "replaced > build.log with | tee build.log"
#   ast:     the transformed tree

# Position objects ({Offset, Line, Col}) are stripped only for comparison;
# the emitted AST keeps them so shfmt --from-json preserves line structure.
def strip_pos:
  walk(if type == "object"
    then with_entries(select(.value
      | (type == "object" and has("Offset") and has("Line") and has("Col"))
      | not))
    else . end);

# First-word literal of a plain command ("" for anything else).
def call_name:
  if (.Type? == "CallExpr") and (((.Args? // []) | length) > 0)
    and (((.Args[0].Parts? // []) | length) == 1)
    and (.Args[0].Parts[0].Type? == "Lit")
  then .Args[0].Parts[0].Value
  else "" end;

# ---------------------------------------------------------------------------
# Fragment rendering: turn removed/rewritten nodes back into text the user
# recognizes from their own command, for systemMessage clauses. Best effort
# for exotic word parts (display only; never used to build the executed
# command).
# ---------------------------------------------------------------------------

def part_text:
  if .Type == "Lit" then .Value
  elif .Type == "SglQuoted" then
    ((if .Dollar == true then "$'" else "'" end) + .Value + "'")
  elif .Type == "DblQuoted" then
    ("\"" + ([(.Parts // [])[] | part_text] | join("")) + "\"")
  elif .Type == "ParamExp" then
    (if .Short == true then ("$" + (.Param.Value // ""))
     else ("${" + (.Param.Value // "") + "}") end)
  elif .Type == "CmdSubst" then "$(...)"
  elif .Type == "ArithmExp" then "$((...))"
  else "..." end;

def word_text: [(.Parts // [])[] | part_text] | join("");

def op_text:
  if . == $ops.gt then ">"
  elif . == $ops.app then ">>"
  elif . == $ops.dup then ">&"
  else "?" end;

def render_redir:
  (.N.Value? // "") + (.Op | op_text) + (.Word | word_text);

# A removed pipeline stage: its command words plus any redirects it carried.
def render_stage:
  (([(.Cmd.Args? // [])[] | word_text]) + [(.Redirs // [])[] | render_redir])
  | join(" ");

# Leading literal text of a word ("" when it starts with an expansion).
def word_lit_prefix:
  if ((.Parts? // []) | length) == 0 then ""
  else .Parts[0] as $p
    | (if ($p.Type == "Lit" or $p.Type == "SglQuoted") then ($p.Value // "")
       elif ($p.Type == "DblQuoted") and ((($p.Parts // []) | length) > 0)
            and ($p.Parts[0].Type == "Lit") then $p.Parts[0].Value
       else "" end)
  end;

# ---------------------------------------------------------------------------
# Heredocs are banned: any << or <<- Redirect node, anywhere in the tree
# (including $(), process substitutions, and compound bodies), denies the
# whole command. The match is restricted to Redirs arrays because the token
# NUMBER for << is shared with the arithmetic shift operator ($((x << 2)) is
# BinaryArithm Op 61 in shfmt 3.8.0) -- a bare Op scan would false-positive.
# Herestrings (<<<) have their own token and are never matched.
# ---------------------------------------------------------------------------

def has_heredoc:
  [.. | objects | select(has("Redirs")) | .Redirs[]
   | select(.Op == $ops.hdoc or .Op == $ops.dashhdoc)]
  | length > 0;

# ---------------------------------------------------------------------------
# Rule (LOUD): scrub stderr-to-/dev/null redirections -- everywhere in the
# tree, including inside command substitutions. Only fd 2 with > or >> and a
# target that is exactly /dev/null (bare, single-quoted, or double-quoted).
# String literals that merely CONTAIN the text are words, not Redirect nodes,
# so they are untouched by construction.
# ---------------------------------------------------------------------------

def is_devnull_target:
  ((.Parts? // []) | length) == 1 and
  (.Parts[0] as $p |
    (($p.Type == "Lit" or $p.Type == "SglQuoted") and $p.Value == "/dev/null")
    or ($p.Type == "DblQuoted" and (($p.Parts // []) | length) == 1
        and $p.Parts[0].Type == "Lit" and $p.Parts[0].Value == "/dev/null"));

def is_stderr_devnull:
  (.N.Value? == "2")
  and (.Op == $ops.gt or .Op == $ops.app)
  and (.Word | is_devnull_target);

def devnull_redirs:
  [.. | objects | select(has("Redirs")) | .Redirs[] | select(is_stderr_devnull)];

def scrub_devnull:
  walk(if type == "object" and has("Redirs")
    then .Redirs |= map(select(is_stderr_devnull | not))
    else . end);

# ---------------------------------------------------------------------------
# Statement surgery helpers.
# ---------------------------------------------------------------------------

# Promote an inner Stmt over $outer, keeping outer-level statement flags.
def promote($outer):
  (if ($outer.Negated? == true) then .Negated = true else . end)
  | (if ($outer.Background? == true) then .Background = true else . end)
  | (if ($outer.Coprocess? == true) then .Coprocess = true else . end)
  | (if ((($outer.Redirs? // []) | length) > 0)
     then .Redirs = ((.Redirs // []) + $outer.Redirs) else . end);

# Strip trailing pipeline stages whose command name is in $names, repeatedly.
# Pipelines parse left-associative, so the last stage is always .Cmd.Y.
# Returns {stmt, removed: [stage Stmts, textual order]}.
def strip_trailing_stages_c($names):
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.pipe)
    and ((.Cmd.Y.Cmd | call_name) as $n | ($names | index($n)) != null)
  then . as $outer
    | (.Cmd.Y) as $stage
    | ((.Cmd.X | promote($outer)) | strip_trailing_stages_c($names)) as $rest
    | {stmt: $rest.stmt, removed: ($rest.removed + [$stage])}
  else {stmt: ., removed: []} end;

# Apply a collector f (Stmt -> {stmt, removed}) to every top-level statement
# and both sides of top-level && / || chains. Never descends into command
# substitutions, process substitutions, subshells, or other compound bodies.
# Returns {ast, removed}.
def on_top_members_c(f):
  def go:
    if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.and or .Cmd.Op == $ops.or)
    then (.Cmd.X | go) as $x | (.Cmd.Y | go) as $y
      | {stmt: ((.Cmd.X = $x.stmt) | (.Cmd.Y = $y.stmt)),
         removed: ($x.removed + $y.removed)}
    else f end;
  if (has("Stmts") | not) then {ast: ., removed: []}
  else
    (reduce .Stmts[] as $s ({stmts: [], removed: []};
       ($s | go) as $r
       | {stmts: (.stmts + [$r.stmt]), removed: (.removed + $r.removed)})) as $acc
    | {ast: (.Stmts = $acc.stmts), removed: $acc.removed}
  end;

# ---------------------------------------------------------------------------
# Legacy trailing rules, anchored where the old text rules anchored: the end
# of the command string, i.e. the last top-level statement (descending the
# right side of its && / || chain, which is a leaf under left association).
# NOTE: strictness settings are never removed -- there is deliberately no
# rule that strips `set -e` or friends.
# ---------------------------------------------------------------------------

def is_bare_true:
  ((.Cmd | call_name) == "true")
  and ((.Cmd.Args | length) == 1)
  and (((.Cmd.Assigns? // []) | length) == 0)
  and (((.Redirs? // []) | length) == 0)
  and (.Negated? != true) and (.Background? != true);

# LOUD: `X || true` at the root of the last statement -> X, repeatedly
# (removing it restores the real exit status).
def strip_or_true_c:
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.or) and (.Cmd.Y | is_bare_true)
  then . as $outer | ((.Cmd.X | promote($outer)) | strip_or_true_c) as $r
    | {stmt: $r.stmt, n: (1 + $r.n)}
  else {stmt: ., n: 0} end;

# SILENT: a trailing `2>&1` (fd 2 dup onto 1) as the LAST redirect,
# repeatedly. Merging stderr into stdout that goes to the transcript anyway
# is plumbing noise, not a behavior change worth announcing.
def is_stderr_merge:
  (.N.Value? == "2") and (.Op == $ops.dup)
  and (((.Word.Parts? // []) | length) == 1)
  and (.Word.Parts[0].Type == "Lit") and (.Word.Parts[0].Value == "1");

def strip_trailing_stderr_merge:
  if (((.Redirs? // []) | length) > 0) and (.Redirs[-1] | is_stderr_merge)
  then (.Redirs |= .[0:-1]) | strip_trailing_stderr_merge
  else . end;

# Apply f to the stage at the textual end of a statement (the last stage of
# a pipeline, or the statement itself).
def on_last_stage(f):
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.pipe or .Cmd.Op == $ops.pipeall)
  then .Cmd.Y |= f
  else f end;

# Apply f at the string-end leaf of the last top-level statement.
def on_spine_leaf(f):
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.and or .Cmd.Op == $ops.or)
  then .Cmd.Y |= f
  else f end;

# Collector variant of the spine-leaf grep strip (LOUD).
def spine_grep_c:
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.and or .Cmd.Op == $ops.or)
  then (.Cmd.Y | strip_trailing_stages_c(["grep"])) as $r
    | {stmt: (.Cmd.Y = $r.stmt), removed: $r.removed}
  else strip_trailing_stages_c(["grep"]) end;

# ---------------------------------------------------------------------------
# Rule (LOUD): rewrite a trailing stdout file redirect into a pipe through
# tee, so the output lands in the file AND stays visible: `cmd > f` ->
# `cmd | tee f`, `cmd >> f` -> `cmd | tee -a f`. Applies to the redirect on
# the final stage of each top-level pipeline/statement (same scope as
# head/tail); the target Word subtree is reused verbatim so quoting and
# expansions are preserved, and every other redirect stays on the producer.
# Exclusions: targets under /dev/ (a stdout discard stays a discard),
# process-substitution targets (> >(cmd)), statements with more than one
# stdout file redirect, anything inside $() or <(). The injected pipefail
# (below) keeps the producer's exit status observable through the pipe.
# ---------------------------------------------------------------------------

def is_stdout_file_any:
  ((.N == null) or (.N.Value? == "1")) and (.Op == $ops.gt or .Op == $ops.app);

def is_stdout_file_teeable:
  is_stdout_file_any
  and (((.Word.Parts? // []) | length) > 0)
  and ((.Word.Parts[0].Type) != "ProcSubst")
  and ((.Word | word_lit_prefix | startswith("/dev/")) | not);

def tee_rewrite_c:
  # . = Stmt -> {stmt, removed: [message clauses]}
  . as $s
  | (($s.Cmd.Type? == "BinaryCmd") and ($s.Cmd.Op == $ops.pipe)) as $is_pipe
  | (if $is_pipe then ($s.Cmd.Y.Redirs // []) else ($s.Redirs // []) end) as $redirs
  | ([$redirs[] | select(is_stdout_file_any)]) as $all
  | if (($all | length) != 1) or (($all[0] | is_stdout_file_teeable) | not)
    then {stmt: $s, removed: []}
    else $all[0] as $r
      | ($r.Op == $ops.app) as $app
      | ($r.Word | word_text) as $ftext
      | ((if $is_pipe
          then ($s | .Cmd.Y.Redirs |= map(select(is_stdout_file_any | not)))
          else ($s | .Redirs |= map(select(is_stdout_file_any | not)))
          end)
         | del(.Negated) | del(.Background) | del(.Coprocess)) as $producer
      | ({Cmd: {Type: "BinaryCmd", Op: $ops.pipe, X: $producer,
                Y: {Cmd: {Type: "CallExpr",
                          Args: ([{Parts: [{Type: "Lit", Value: "tee"}]}]
                                 + (if $app then [{Parts: [{Type: "Lit", Value: "-a"}]}] else [] end)
                                 + [$r.Word])}}}}
         + (if ($s.Negated? == true) then {Negated: true} else {} end)
         + (if ($s.Background? == true) then {Background: true} else {} end)
         + (if ($s.Coprocess? == true) then {Coprocess: true} else {} end)) as $new
      | {stmt: $new,
         removed: [("replaced " + (if $app then ">> " else "> " end) + $ftext
                    + " with | tee " + (if $app then "-a " else "" end) + $ftext)]}
    end;

# ---------------------------------------------------------------------------
# Rule (SILENT): ensure `set -o pipefail` on every command. If the first
# top-level statement (or the leftmost leaf of its && chain) is a `set` call
# that already enables pipefail (set -o pipefail, set -eo pipefail,
# set -euo pipefail, set -e -o pipefail, multiple -o pairs), do nothing;
# otherwise prepend a `set -o pipefail` statement.
# ---------------------------------------------------------------------------

def enables_pipefail:
  ((.Cmd | call_name) == "set")
  and (([(.Cmd.Args[1:] // [])[]
         | if (((.Parts? // []) | length) == 1 and (.Parts[0].Type == "Lit"))
           then .Parts[0].Value else " " end]) as $a
       | (($a | length) >= 2)
         and any(range(0; ($a | length) - 1);
                 ($a[.] | test("^-[A-Za-z]*o$")) and ($a[. + 1] == "pipefail")));

def first_leaf_enables_pipefail:
  def leftmost:
    if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.and)
    then (.Cmd.X | leftmost)
    else . end;
  (leftmost | enables_pipefail);

def pipefail_stmt:
  {Cmd: {Type: "CallExpr", Args: [
    {Parts: [{Type: "Lit", Value: "set"}]},
    {Parts: [{Type: "Lit", Value: "-o"}]},
    {Parts: [{Type: "Lit", Value: "pipefail"}]}]}};

def ensure_pipefail:
  if (has("Stmts") | not) or ((.Stmts | length) == 0) then .
  elif (.Stmts[0] | first_leaf_enables_pipefail) then .
  else .Stmts = ([pipefail_stmt] + .Stmts) end;

# ---------------------------------------------------------------------------
# Assemble. State: {ast, fired, clauses}. Each step compares before/after and
# records what fired; loud steps also record message clauses rendered from
# the actual removed/rewritten nodes.
# ---------------------------------------------------------------------------

def step_devnull:
  . as $st
  | ($st.ast | devnull_redirs) as $rm
  | if ($rm | length) == 0 then $st
    else {ast: ($st.ast | scrub_devnull),
          fired: ($st.fired + ["devnull"]),
          clauses: ($st.clauses + ($rm | map("removed " + render_redir)))}
    end;

def step_head_tail:
  . as $st
  | ($st.ast | on_top_members_c(strip_trailing_stages_c(["head", "tail"]))) as $r
  | if ($r.removed | length) == 0 then $st
    else {ast: $r.ast,
          fired: ($st.fired + ["head_tail"]),
          clauses: ($st.clauses + ($r.removed | map("removed | " + render_stage)))}
    end;

def step_or_true:
  . as $st
  | if ($st.ast | has("Stmts") | not) or (($st.ast.Stmts | length) == 0) then $st
    else ($st.ast.Stmts[-1] | strip_or_true_c) as $r
    | if $r.n == 0 then $st
      else {ast: ($st.ast | .Stmts[-1] = $r.stmt),
            fired: ($st.fired + ["or_true"]),
            clauses: ($st.clauses + ["removed || true"])}
      end
    end;

def step_grep:
  . as $st
  | if ($st.ast | has("Stmts") | not) or (($st.ast.Stmts | length) == 0) then $st
    else ($st.ast.Stmts[-1] | spine_grep_c) as $r
    | if ($r.removed | length) == 0 then $st
      else {ast: ($st.ast | .Stmts[-1] = $r.stmt),
            fired: ($st.fired + ["grep"]),
            clauses: ($st.clauses + ($r.removed | map("removed | " + render_stage)))}
      end
    end;

def step_stderr_merge:
  . as $st
  | if ($st.ast | has("Stmts") | not) or (($st.ast.Stmts | length) == 0) then $st
    else ($st.ast | .Stmts[-1] |= on_spine_leaf(on_last_stage(strip_trailing_stderr_merge))) as $a
    | if (($a | strip_pos) == ($st.ast | strip_pos)) then $st
      else {ast: $a, fired: ($st.fired + ["stderr_merge"]), clauses: $st.clauses}
      end
    end;

def step_tee:
  . as $st
  | ($st.ast | on_top_members_c(tee_rewrite_c)) as $r
  | if ($r.removed | length) == 0 then $st
    else {ast: $r.ast,
          fired: ($st.fired + ["tee"]),
          clauses: ($st.clauses + $r.removed)}
    end;

def step_pipefail:
  . as $st
  | ($st.ast | ensure_pipefail) as $a
  | if (($a | strip_pos) == ($st.ast | strip_pos)) then $st
    else {ast: $a, fired: ($st.fired + ["pipefail"]), clauses: $st.clauses}
    end;

def pass_once:
  step_devnull | step_head_tail | step_or_true | step_grep
  | step_stderr_merge | step_tee;

def fix_state:
  . as $st
  | (pass_once) as $next
  | if (($next.ast | strip_pos) == ($st.ast | strip_pos)) then $st
    else ($next | fix_state) end;

def dedupe:
  reduce .[] as $c ([]; if index($c) then . else . + [$c] end);

. as $orig
| if has_heredoc then {deny: true, changed: false, silent: false, message: "", ast: $orig}
  else
    ({ast: $orig, fired: [], clauses: []} | fix_state | step_pipefail) as $st
    | ($st.clauses | dedupe) as $cl
    | (($st.ast | strip_pos) != ($orig | strip_pos)) as $changed
    | {deny: false,
       changed: $changed,
       silent: ($changed and (($cl | length) == 0)),
       message: ($cl | join("; ")),
       ast: $st.ast}
  end
