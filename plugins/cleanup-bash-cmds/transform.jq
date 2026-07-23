# cleanup-bash-cmds AST transform (driven by hook.sh).
#
# Input: a shfmt --to-json syntax tree (mvdan.cc/sh typed JSON).
# $ops:  operator token numbers probed at runtime from the SAME shfmt binary
#        ({gt, app, dup, and, or, pipe, pipeall, hdoc, dashhdoc}). The
#        numeric values differ between shfmt versions (e.g. "|" is 12 in
#        v3.8.0 but 13 in v3.13.1), so they must never be hardcoded.
# Output: {deny, changed, rules, ast}
#   deny:    command contains a heredoc; block it (nothing else applies)
#   changed: the tree semantically changed (a rewrite should be emitted)
#   rules:   comma-joined names of the rules that fired (for the debug log
#            ONLY -- the hook is deliberately silent toward user and model)
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
# Rule: scrub stderr-to-/dev/null redirections -- everywhere in the tree,
# including inside command substitutions. Only fd 2 with > or >> and a
# target that is exactly /dev/null (bare, single-quoted, or double-quoted).
# String literals that merely CONTAIN the text are words, not Redirect
# nodes, so they are untouched by construction.
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
# Applied only at the textual end of the command (on_last_stmt +
# on_spine_leaf, below): a limiting pipe on a NON-final statement is an
# intentional part of a longer script and is preserved.
def strip_trailing_stages($names):
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.pipe)
    and ((.Cmd.Y.Cmd | call_name) as $n | ($names | index($n)) != null)
  then . as $outer | (.Cmd.X | promote($outer)) | strip_trailing_stages($names)
  else . end;

# ---------------------------------------------------------------------------
# Trailing-noise rules, all anchored where the old text rules anchored: the
# end of the command string, i.e. the last top-level statement (descending
# the right side of its && / || chain, which is a leaf under left
# association). head_tail and tee share this anchoring too (see pass_once):
# mid-script `| tail -N` / `> file` in a longer script are deliberate and
# stay untouched.
# NOTE: strictness settings are never removed -- there is deliberately no
# rule that strips `set -e` or friends.
# ---------------------------------------------------------------------------

def is_bare_true:
  ((.Cmd | call_name) == "true")
  and ((.Cmd.Args | length) == 1)
  and (((.Cmd.Assigns? // []) | length) == 0)
  and (((.Redirs? // []) | length) == 0)
  and (.Negated? != true) and (.Background? != true);

# `X || true` at the root of the last statement -> X, repeatedly (removing
# it restores the real exit status).
def strip_or_true:
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.or) and (.Cmd.Y | is_bare_true)
  then . as $outer | (.Cmd.X | promote($outer)) | strip_or_true
  else . end;

# A trailing `2>&1` (fd 2 dup onto 1) as the LAST redirect, repeatedly.
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

def on_last_stmt(f):
  if (has("Stmts") | not) or ((.Stmts | length) == 0) then .
  else .Stmts[-1] |= f end;

# ---------------------------------------------------------------------------
# Rule: rewrite a trailing stdout file redirect into a pipe through tee, so
# the output lands in the file AND stays visible: `cmd > f` -> `cmd | tee f`,
# `cmd >> f` -> `cmd | tee -a f`. Applies only to the FINAL top-level
# statement, at the rightmost leaf of its && / || spine (same anchoring as
# head/tail and grep); a mid-script `> file` is a deliberate part of a
# longer script and is preserved. The target Word subtree is reused verbatim
# so quoting and expansions are preserved, and every other redirect stays on
# the producer. Exclusions: targets under /dev/ (a stdout discard stays a
# discard), process-substitution targets (> >(cmd)), statements with more
# than one stdout file redirect, anything inside $() or <(). The injected
# pipefail (below) keeps the producer's exit status observable through the
# pipe.
# ---------------------------------------------------------------------------

def is_stdout_file_any:
  ((.N == null) or (.N.Value? == "1")) and (.Op == $ops.gt or .Op == $ops.app);

def is_stdout_file_teeable:
  is_stdout_file_any
  and (((.Word.Parts? // []) | length) > 0)
  and ((.Word.Parts[0].Type) != "ProcSubst")
  and ((.Word | word_lit_prefix | startswith("/dev/")) | not);

def tee_rewrite:
  # . = Stmt
  . as $s
  | (($s.Cmd.Type? == "BinaryCmd") and ($s.Cmd.Op == $ops.pipe)) as $is_pipe
  | (if $is_pipe then ($s.Cmd.Y.Redirs // []) else ($s.Redirs // []) end) as $redirs
  | ([$redirs[] | select(is_stdout_file_any)]) as $all
  | if (($all | length) != 1) or (($all[0] | is_stdout_file_teeable) | not)
    then $s
    else $all[0] as $r
      | ((if $is_pipe
          then ($s | .Cmd.Y.Redirs |= map(select(is_stdout_file_any | not)))
          else ($s | .Redirs |= map(select(is_stdout_file_any | not)))
          end)
         | del(.Negated) | del(.Background) | del(.Coprocess)) as $producer
      | ({Cmd: {Type: "BinaryCmd", Op: $ops.pipe, X: $producer,
                Y: {Cmd: {Type: "CallExpr",
                          Args: ([{Parts: [{Type: "Lit", Value: "tee"}]}]
                                 + (if ($r.Op == $ops.app) then [{Parts: [{Type: "Lit", Value: "-a"}]}] else [] end)
                                 + [$r.Word])}}}}
         + (if ($s.Negated? == true) then {Negated: true} else {} end)
         + (if ($s.Background? == true) then {Background: true} else {} end)
         + (if ($s.Coprocess? == true) then {Coprocess: true} else {} end))
    end;

# ---------------------------------------------------------------------------
# Rule: ensure `set -o pipefail` on every command. If the first top-level
# statement (or the leftmost leaf of its && chain) is a `set` call that
# already enables pipefail (set -o pipefail, set -eo pipefail,
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
# Rule: cap sleep durations at 3 seconds -- everywhere in the tree, including
# $() / <() captures, loops, functions, subshells, and both sides of
# && / || / ; chains. A CallExpr in real command position whose command word
# is the plain literal `sleep` (prefix env assignments still count) keeps its
# argument list only when EVERY argument is a literal word (no expansions)
# that parses as a GNU sleep duration (float with optional s/m/h/d suffix)
# and the durations sum to <= 3 seconds. Anything else -- over-cap totals,
# `infinity`/`inf` (they fail the number pattern, deliberately sharing the
# junk path), $VAR / $() arguments, unparseable junk, zero arguments --
# replaces the WHOLE argument list with the single literal `3`. Word
# arguments to OTHER commands (`timeout 5 sleep 30`) and "sleep" inside
# string literals are untouched by construction: only command position
# matches call_name. Node-local: only .Args of the matched CallExpr changes;
# assignments, redirects, and statement structure are never touched, so the
# rule cannot drop or reorder statements.
# ---------------------------------------------------------------------------

# Literal text of a word: every part must be a Lit, SglQuoted (incl. $'..'),
# or DblQuoted over Lits. null when the word contains any expansion.
def word_literal:
  if ((.Parts? // []) | length) == 0 then null
  else
    ([.Parts[]
      | if (.Type == "Lit" or .Type == "SglQuoted") then (.Value // "")
        elif (.Type == "DblQuoted") and ((.Parts // []) | all(.Type == "Lit"))
        then ([(.Parts // [])[] | (.Value // "")] | join(""))
        else null end]) as $texts
    | if ($texts | any(. == null)) then null else ($texts | join("")) end
  end;

# GNU sleep duration in seconds; null when unparseable. Deliberately strict:
# plain decimals with an optional s/m/h/d suffix. Scientific notation,
# signs, inf/infinity, and junk all yield null (=> capped to `sleep 3`).
def sleep_seconds:
  (capture("^(?<n>[0-9]+(\\.[0-9]*)?|\\.[0-9]+)(?<u>[smhd]?)$") // null) as $m
  | if $m == null then null
    else ($m.n | tonumber) * ({s: 1, m: 60, h: 3600, d: 86400}[$m.u] // 1)
    end;

def cap_sleep_call:
  # . = CallExpr already known to be a `sleep` command.
  ([.Args[1:][] | word_literal as $lit
    | if $lit == null then null else ($lit | sleep_seconds) end]) as $secs
  | if (($secs | length) > 0) and ($secs | all(. != null))
       and (($secs | add) <= 3)
    then .
    else .Args = [.Args[0], {Parts: [{Type: "Lit", Value: "3"}]}]
    end;

def cap_sleep:
  walk(if (type == "object") and (call_name == "sleep")
    then cap_sleep_call
    else . end);

# ---------------------------------------------------------------------------
# Rule: REMOVE constant narration echoes/printfs. A CallExpr whose EFFECTIVE
# command (via effective_command, so `command echo`, `builtin printf`,
# `\echo`, `"printf"` all resolve) is `echo` or `printf`, and whose stdout
# actually reaches the terminal, has its ENTIRE .Cmd replaced by the no-op
# `:` (colon_cmd) -- no output, exit status 0, surrounding statement structure
# and redirects untouched. `:` is used instead of deleting the statement so the
# top-level statement count never drops (the hook's no-lost-statement guard
# stays satisfied) and the command still exits 0.
#
#   - echo: removed when every argument after the command word is constant
#     (word_is_constant: no ParamExp / CmdSubst / ArithmExp, no glob/brace/~;
#     flags like -n / -e and pure-literal quoted strings count as constant).
#     Bare `echo` (no args) is removed too.
#   - printf: removed ONLY for the literal-print form -- exactly ONE argument
#     after the command word, a static string (word_literal != null) with NO
#     `%` in it. A `%` directive (`printf '%s'`, `printf '%d' 5`), `%%`, extra
#     args beyond the format (`printf '%s\n' hi`), or a `$var` / `$()` argument
#     all leave printf untouched -- that printf is really formatting, not
#     narration.
#
# stdout reaches the terminal iff, walking TOP-DOWN from the file root:
#   - the statement is not the X side of a | or |& (that stdout feeds the
#     pipe: `echo '{}' | jq` is data);
#   - no statement on the path (the echo's own, or an enclosing compound's)
#     carries a redirect other than a pure stderr one -- allowed are 2>f,
#     2>>f, and 2>&n only; > >> >&n &> >| < <<< etc. all make the subtree
#     invisible (conservative: unknown redirect = no rewrite);
#   - it is not inside $(), backticks, <() or >( ) -- those live inside Word
#     parts, which this traversal never enters, so captures are excluded by
#     construction;
#   - it is not inside a FuncDecl body (the call site decides visibility --
#     `x=$(f)` would capture -- so function bodies are conservatively
#     skipped) or a coproc (its stdout is captured by the coproc fd).
# Compound bodies (blocks, subshells, if/while/for/case, time) stay visible
# unless one of the rules above flips them off; both sides of && / || / ;
# count as statement position. Node-local: only the matched CallExpr's .Cmd is
# replaced, so the rule cannot drop or reorder statements.
# ---------------------------------------------------------------------------

# The no-op replacement: a bare `:` CallExpr (Args = [":"]). Replaces a matched
# narration command's .Cmd; the enclosing Stmt's Redirs are left in place, so
# `echo warn 2>>err.log` becomes `: 2>>err.log`. Shape taken from
# `printf ':' | shfmt --to-json | jq '.Stmts[0].Cmd'` (positions dropped, as
# with every other node synthesized here).
def colon_cmd:
  {Type: "CallExpr", Args: [{Parts: [{Type: "Lit", Value: ":"}]}]};

# A word is constant when every part is a Lit without glob/expansion risk
# (* ? [ { trigger pathname/brace expansion; leading ~ expands to $HOME), a
# single-quoted string (incl. $'..'), or a double-quoted string over Lits.
def word_is_constant:
  def part_constant:
    if .Type == "Lit"
    then ((.Value // "") | (test("[*?\\[{]") or startswith("~")) | not)
    elif .Type == "SglQuoted" then true
    elif .Type == "DblQuoted" then ((.Parts // []) | all(.Type == "Lit"))
    else false end;
  ((.Parts? // []) | all(part_constant));

# Every redirect on the statement leaves stdout alone: fd 2 with > >> or >&
# only. Anything else (stdout redirects, &>, fd juggling, stdin forms) is
# disqualifying -- unknown ops fail closed into "leave the echo alone".
def redirs_stderr_only:
  ((.Redirs? // []) | all(
    (.N.Value? == "2")
    and (.Op == $ops.gt or .Op == $ops.app or .Op == $ops.dup)));

# ---------------------------------------------------------------------------
# Effective-command resolver. Given a CallExpr, work out the command that
# actually runs -- seeing through a quoted/split command word ("printf",
# pri'ntf', 'echo'), a single leading backslash (\echo -> echo), and the
# `command`/`builtin` wrappers (recursively, so `command command echo` and
# `command -p printf` resolve to their target). Returns {name, index} where
# `index` is the position of the effective command word in the ORIGINAL .Args
# (so a caller can preserve the wrapper + flags), or null for "no effective
# command": a non-static command word ($x, `x`, $(x)) or a lookup invocation
# (`command -v NAME` / `-V`, including bundled forms whose letters contain v
# or V -- those print a name, they do not execute it). Uses word_literal for
# the static-string test (every part a Lit / SglQuoted / DblQuoted-over-Lits;
# any expansion => null). Never descends into Word parts, so a command name
# that merely APPEARS as an argument is not resolved.
def effective_command:
  . as $call
  | ($call.Args // []) as $args
  | def resolve($i; $depth):
      if $depth <= 0 then null
      elif $i >= ($args | length) then null
      else ($args[$i] | word_literal) as $raw
        | if $raw == null then null                # non-static command word
          else
            (if ($raw | startswith("\\")) then ($raw[1:]) else $raw end) as $w
            | if ($w == "command") or ($w == "builtin")
              then
                (($w == "command")) as $is_command
                | # Scan leading flags after the wrapper; return the index of
                  # the target command word, "LOOKUP", or null.
                  def scan($j):
                    if $j >= ($args | length) then null       # only flags, no cmd
                    else ($args[$j] | word_literal) as $fl
                      | if $fl == null then null              # non-static -> bail
                        elif $fl == "--" then ($j + 1)         # end of flags
                        elif ($fl | startswith("-")) and (($fl | length) > 1)
                        then
                          if $is_command and ($fl | test("[vV]"))
                          then "LOOKUP"                        # command -v/-V (bundled too)
                          else scan($j + 1)                     # benign flag (-p): skip
                          end
                        else $j                                 # non-flag: command word
                        end
                    end;
                  (scan($i + 1)) as $next
                  | if $next == null then null
                    elif $next == "LOOKUP" then null
                    else resolve($next; $depth - 1)             # recurse into target
                    end
              else {name: $w, index: $i}
              end
          end
      end;
    resolve(0; 10);

# ---------------------------------------------------------------------------
# Rule: perl is banned. Any statement whose EFFECTIVE command name (via the
# resolver above, so `command perl`, `\perl`, and `perl5.36` all count while
# `command -v perl` does not) matches the anchored regex ^perl[0-9.]*$ denies
# the whole command. Unlike narration_remove there is NO visibility guard --
# perl anywhere is execution. Word-part scoping (see any_call_in_stmts) keeps
# `grep perl file` and `perlcritic` (perl as an argument / a different
# command) unmatched.
# Shared deny walker: does any CallExpr in statement position satisfy pred?
# Mirrors narration_remove over statement structure (both sides of pipes /
# && / ||, blocks, subshells, if/while/for/case/time) and additionally
# descends FuncDecl bodies (a banned call defined in a function still runs
# when the function is called). It never enters Word parts, so a command
# name that merely APPEARS as an argument or inside $() / <() is not
# matched. Used by the perl deny and the file-read deny below.
def any_call_in_stmts(pred):
  def stmt_hit:
    # . = Stmt
    if (has("Cmd") | not) or (.Cmd == null) then false
    else .Cmd
      | if .Type? == "CallExpr" then
          pred
        elif .Type? == "BinaryCmd" then
          (.X | stmt_hit) or (.Y | stmt_hit)
        elif (.Type? == "Block") or (.Type? == "Subshell") then
          ((.Stmts // []) | any(stmt_hit))
        elif .Type? == "WhileClause" then
          (((.Cond // []) | any(stmt_hit))) or (((.Do // []) | any(stmt_hit)))
        elif .Type? == "ForClause" then
          ((.Do // []) | any(stmt_hit))
        elif .Type? == "IfClause" then
          def if_chain:
            (((.Cond // []) | any(stmt_hit))
             or ((.Then // []) | any(stmt_hit))
             or (if (has("Else") and (.Else != null))
                 then (.Else | if_chain) else false end));
          if_chain
        elif .Type? == "CaseClause" then
          ((.Items // []) | any((.Stmts // []) | any(stmt_hit)))
        elif .Type? == "TimeClause" then
          (if (has("Stmt") and (.Stmt != null)) then (.Stmt | stmt_hit) else false end)
        elif .Type? == "FuncDecl" then
          (if (has("Body") and (.Body != null)) then (.Body | stmt_hit) else false end)
        else false end
    end;
  if has("Stmts") then (.Stmts | any(stmt_hit)) else false end;

def has_perl_invocation:
  def is_perl: (. != null) and test("^perl[0-9.]*$");
  any_call_in_stmts((effective_command) as $ec
    | ($ec != null) and ($ec.name | is_perl));

def narration_remove:
  # $vis threads "stdout reaches the terminal" top-down; jq's walk is
  # bottom-up with no ancestor info, so the traversal is hand-rolled over
  # statement structure only (never into Words -- captures stay data).
  def remove_stmt($vis):
    # . = Stmt
    ($vis and redirs_stderr_only and (.Coprocess? != true)) as $v
    | if (has("Cmd") | not) or (.Cmd == null) then .
      else .Cmd |= (
        if .Type? == "CallExpr" then
          (effective_command) as $ec
          | if $v and ($ec != null)
               and (
                 (($ec.name == "echo")
                  and ((.Args[$ec.index + 1:]) | all(word_is_constant)))
                 or (($ec.name == "printf")
                     and (((.Args[$ec.index + 1:]) | length) == 1)
                     and (((.Args[$ec.index + 1] | word_literal)) as $s
                          | ($s != null) and (($s | test("%")) | not))))
            then colon_cmd
            else . end
        elif .Type? == "BinaryCmd" then
          if (.Op == $ops.pipe or .Op == $ops.pipeall)
          then (.X |= remove_stmt(false)) | (.Y |= remove_stmt($v))
          elif (.Op == $ops.and or .Op == $ops.or)
          then (.X |= remove_stmt($v)) | (.Y |= remove_stmt($v))
          else . end
        elif (.Type? == "Block") or (.Type? == "Subshell") then
          .Stmts |= map(remove_stmt($v))
        elif .Type? == "WhileClause" then
          (.Cond |= map(remove_stmt($v))) | (.Do |= map(remove_stmt($v)))
        elif .Type? == "ForClause" then
          .Do |= map(remove_stmt($v))
        elif .Type? == "IfClause" then
          # The elif/else chain: Else nodes are IfClauses without a Type
          # field (and a plain else has no Cond), so walk the chain by
          # field presence.
          def remove_ifchain:
            (if has("Cond") and (.Cond != null)
             then .Cond |= map(remove_stmt($v)) else . end)
            | (if has("Then") and (.Then != null)
               then .Then |= map(remove_stmt($v)) else . end)
            | (if has("Else") and (.Else != null)
               then .Else |= remove_ifchain else . end);
          remove_ifchain
        elif .Type? == "CaseClause" then
          .Items |= map(if has("Stmts") and (.Stmts != null)
                        then .Stmts |= map(remove_stmt($v)) else . end)
        elif .Type? == "TimeClause" then
          (if has("Stmt") and (.Stmt != null)
           then .Stmt |= remove_stmt($v) else . end)
        else . end)  # FuncDecl, CoprocClause, DeclClause, ...: leaf
      end;
  if has("Stmts") then .Stmts |= map(remove_stmt(true)) else . end;

# ---------------------------------------------------------------------------
# Rule: reading files with cat/head/tail is banned -- use the Read tool.
# Any CallExpr in statement position (same walk as the perl deny: pipes,
# && / ||, compounds, FuncDecl bodies; never Word parts) whose EFFECTIVE
# command is cat, head, or tail AND that names at least one static,
# non-"magic" file operand denies the whole command. `cd x && head -60 f`
# is the incident shape: no pipe for the stage-strip rule to see, and the
# model should have used the Read tool.
#
# What still runs (NOT a file read, or not resolvable statically):
#   - "magic" pseudo-file operands: paths under /proc, /sys, or /dev
#     (`cat /proc/meminfo`, `head -c 100 /dev/urandom` -- the Read tool
#     cannot meaningfully read those, and truncating an unbounded stream
#     is legitimate there)
#   - no operands / stdin only: `x | cat`, `cmd | head -5`, `cat -`
#   - process-substitution operands: `cat <(cmd)` is stream plumbing
#   - operands carrying any expansion (`cat "$F"`): not statically known,
#     same fail-open posture as the rest of the hook
#   - cat/head/tail inside $() / <() word contexts (`x=$(cat f)`): capture
#     is scripting, and the walk never enters Word parts anyway (the same
#     scoping that keeps `grep perl` out of the perl deny)
# Flags are skipped when finding operands, including the separated value
# of -n / -c / -s, bundled clusters ending in a value-taking letter
# (-qn 3), value-taking GNU long forms (--lines 20), and old-style limits
# (-60, +5), so `head -n 20 /proc/meminfo` does not mistake `20` for a
# file. cat has no value-taking flags, so for it every dash word is just
# dropped.
# ---------------------------------------------------------------------------

# Operand words of a cat/head/tail call. $args excludes the command word;
# $valued enables head/tail's value-taking flag handling. `--` ends flag
# parsing; a lone `-` is stdin, not a file, and is not an operand here.
def read_operands($args; $valued):
  reduce $args[] as $w ({ops: [], skip: false, done: false};
    if .skip then .skip = false
    elif .done then .ops += [$w]
    else ($w | word_literal) as $lit
      | if $lit == null then .ops += [$w]
        elif $lit == "--" then .done = true
        elif $lit == "-" then .
        elif ($lit | startswith("--")) then
          if $valued and ($lit | test("^--(lines|bytes|sleep-interval|pid|max-unchanged-stats)$"))
          then .skip = true else . end
        elif ($lit | startswith("-")) and (($lit | length) > 1) then
          if $valued and ($lit | test("^-[A-Za-z]*[ncs]$")) then .skip = true else . end
        elif $valued and ($lit | test("^\\+[0-9]+$")) then .
        else .ops += [$w] end
      end)
  | .ops;

def is_magic_path: test("^/(proc|sys|dev)(/|$)");

def call_reads_banned_file:
  # . = CallExpr
  (effective_command) as $ec
  | ($ec != null)
    and ($ec.name == "cat" or $ec.name == "head" or $ec.name == "tail")
    and (read_operands(.Args[$ec.index + 1:]; ($ec.name != "cat"))
         | any(.[]; (word_literal) as $lit
               | ($lit != null) and (($lit | is_magic_path) | not)));

def has_banned_file_read:
  any_call_in_stmts(call_reads_banned_file);

# ---------------------------------------------------------------------------
# Assemble. State: {ast, fired}. Each step compares before/after (positions
# stripped) and records the rule name when it changed something. The fired
# list feeds ONLY the CLEANUP_BASH_CMDS_LOG debug log; the hook never
# announces rewrites to the user or the model.
# ---------------------------------------------------------------------------

def apply_step($name; f):
  . as $st
  | ($st.ast | f) as $a
  | if (($a | strip_pos) == ($st.ast | strip_pos)) then $st
    else {ast: $a, fired: ($st.fired + [$name])} end;

def pass_once:
  apply_step("devnull"; scrub_devnull)
  | apply_step("head_tail"; on_last_stmt(on_spine_leaf(strip_trailing_stages(["head", "tail"]))))
  | apply_step("or_true"; on_last_stmt(strip_or_true))
  | apply_step("grep"; on_last_stmt(on_spine_leaf(strip_trailing_stages(["grep"]))))
  | apply_step("stderr_merge"; on_last_stmt(on_spine_leaf(on_last_stage(strip_trailing_stderr_merge))))
  | apply_step("tee"; on_last_stmt(on_spine_leaf(tee_rewrite)))
  | apply_step("sleep_cap"; cap_sleep)
  | apply_step("narration_remove"; narration_remove);

def fix_state:
  . as $st
  | (pass_once) as $next
  | if (($next.ast | strip_pos) == ($st.ast | strip_pos)) then $st
    else ($next | fix_state) end;

def dedupe:
  reduce .[] as $c ([]; if index($c) then . else . + [$c] end);

. as $orig
| if has_heredoc then {deny: true, changed: false, rules: "heredoc", ast: $orig}
  elif has_perl_invocation then {deny: true, changed: false, rules: "perl", ast: $orig}
  elif has_banned_file_read then {deny: true, changed: false, rules: "file_read", ast: $orig}
  else
    ({ast: $orig, fired: []} | fix_state | apply_step("pipefail"; ensure_pipefail)) as $st
    | {deny: false,
       changed: (($st.ast | strip_pos) != ($orig | strip_pos)),
       rules: ($st.fired | dedupe | join(",")),
       ast: $st.ast}
  end
