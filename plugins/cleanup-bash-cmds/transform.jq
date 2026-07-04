# cleanup-bash-cmds AST transform (driven by hook.sh).
#
# Input: a shfmt --to-json syntax tree (mvdan.cc/sh typed JSON).
# $ops:  operator token numbers probed at runtime from the SAME shfmt binary
#        ({gt, app, dup, and, or, pipe, pipeall, hdoc, dashhdoc}). The
#        numeric values differ between shfmt versions (e.g. "|" is 12 in
#        v3.8.0 but 13 in v3.13.1), so they must never be hardcoded.
# Output: {deny: bool, changed: bool, ast: <transformed tree>}.
#         deny=true means the command contains a heredoc and must be blocked;
#         changed/ast are only meaningful when deny is false.

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
# Scrub stderr-to-/dev/null redirections -- everywhere in the tree, including
# inside command substitutions. Only fd 2 with > or >> and a target that is
# exactly /dev/null (bare, single-quoted, or double-quoted). String literals
# and heredoc bodies that merely CONTAIN the text "2>/dev/null" are words,
# not Redirect nodes, so they are untouched by construction.
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
def strip_trailing_stages($names):
  if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.pipe)
    and ((.Cmd.Y.Cmd | call_name) as $n | ($names | index($n)) != null)
  then . as $outer | (.Cmd.X | promote($outer)) | strip_trailing_stages($names)
  else . end;

# ---------------------------------------------------------------------------
# Kill trailing | head / | tail stages (any flags/arguments), until stable.
# Scope: every top-level statement and both sides of top-level && / ||
# chains. Never descends into command substitutions, process substitutions,
# subshells, or other compound bodies -- $(ls | head -1) is functional
# capture, not output truncation.
# ---------------------------------------------------------------------------

def strip_head_tail:
  def go:
    if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.and or .Cmd.Op == $ops.or)
    then (.Cmd.X |= go) | (.Cmd.Y |= go)
    else strip_trailing_stages(["head", "tail"]) end;
  if has("Stmts") then .Stmts |= map(go) else . end;

# ---------------------------------------------------------------------------
# Legacy rules, anchored where the old text rules anchored: the end of the
# command string, i.e. the last top-level statement (descending the right
# side of its && / || chain, which is always a leaf under left association).
# ---------------------------------------------------------------------------

def is_bare_true:
  ((.Cmd | call_name) == "true")
  and ((.Cmd.Args | length) == 1)
  and (((.Cmd.Assigns? // []) | length) == 0)
  and (((.Redirs? // []) | length) == 0)
  and (.Negated? != true) and (.Background? != true);

# `X || true` at the root of the last statement -> X, repeatedly.
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

def spine_leaf_rules:
  strip_trailing_stages(["grep"]) | on_last_stage(strip_trailing_stderr_merge);

def last_stmt_legacy_rules:
  strip_or_true
  | if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.and or .Cmd.Op == $ops.or)
    then .Cmd.Y |= spine_leaf_rules
    else spine_leaf_rules end;

def apply_last_stmt_rules:
  if has("Stmts") and ((.Stmts | length) > 0)
  then .Stmts[-1] |= last_stmt_legacy_rules
  else . end;

# ---------------------------------------------------------------------------
# Legacy rules: leading `set -e; ` and `set -e && `. The old text rule keyed
# on the explicit separator, so the semicolon form requires Stmt.Semicolon
# (a bare `set -e` on its own line stays, exactly as before), and the whole
# command must contain something else (never emit an empty command).
# ---------------------------------------------------------------------------

def is_set_e:
  ((.Cmd | call_name) == "set")
  and ((.Cmd.Args | length) == 2)
  and (((.Cmd.Args[1].Parts? // []) | length) == 1)
  and (.Cmd.Args[1].Parts[0].Type == "Lit")
  and (.Cmd.Args[1].Parts[0].Value == "-e")
  and (((.Cmd.Assigns? // []) | length) == 0)
  and (((.Redirs? // []) | length) == 0)
  and (.Negated? != true) and (.Background? != true);

def strip_leading_set_e:
  if (has("Stmts") | not) then . else
    (if ((.Stmts | length) > 1) and (.Stmts[0] | is_set_e) and (.Stmts[0].Semicolon != null)
     then .Stmts |= .[1:] else . end)
    | (if ((.Stmts | length) > 0)
       then .Stmts[0] |= (
         def snip:
           if (.Cmd.Type? == "BinaryCmd") and (.Cmd.Op == $ops.and)
           then (if (.Cmd.X | is_set_e)
                 then . as $outer | (.Cmd.Y | promote($outer))
                 else .Cmd.X |= snip end)
           else . end;
         snip)
       else . end)
  end;

# ---------------------------------------------------------------------------
# Assemble: run every rule to a fixpoint (each rule only deletes nodes, so
# this terminates), then report whether anything semantically changed.
# ---------------------------------------------------------------------------

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

def transform_once:
  scrub_devnull
  | strip_head_tail
  | apply_last_stmt_rules
  | strip_leading_set_e;

def fixpoint(f):
  . as $x | f as $y
  | if (($y | strip_pos) == ($x | strip_pos)) then $x else ($y | fixpoint(f)) end;

. as $orig
| if has_heredoc then {deny: true, changed: false, ast: $orig}
  else
    (fixpoint(transform_once) as $new
     | {deny: false, changed: (($orig | strip_pos) != ($new | strip_pos)), ast: $new})
  end
