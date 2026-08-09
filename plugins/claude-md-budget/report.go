// The three reports: the session-start census, the post-edit notice, and the
// Stop refusal. All three prescribe the same remedy, because there is only one.

package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// widthOnly is a file whose ONLY offense is unwrapped lines: comfortably under
// budget, nothing to extract. Reporting it in budget language is a false alarm,
// and a guard that cries wolf gets skimmed on the run where the number is real.
func widthOnly(o offender, limit int) bool {
	return len(o.Wide) > 0 && o.Chars < nearLimit(limit)
}

func allWidthOnly(os []offender, limit int) bool {
	for _, o := range os {
		if !widthOnly(o, limit) {
			return false
		}
	}
	return len(os) > 0
}

// remedy is identical wherever it is reported from, EXCEPT that a width-only
// finding gets only the bullets that apply to it: telling a session to extract
// prose from a file that is 7% under budget is advice it cannot act on, and
// wading through it is what teaches the reader to skim the whole block.
func remedy() []string {
	return remedyFor(false)
}

func remedyFor(widthOnly bool) []string {
	if widthOnly {
		return []string{
			"What to do about it:",
			"- Hard-wrap at " + strconv.Itoa(widthLimit) + " columns. Nothing needs to be extracted -- the file " +
				"is under budget; the lines are just unreadable in a diff.",
			"- Continuation lines of a list item indent to the marker. Code fences, tables and " +
				"headings are exempt, and a single unbreakable token (a URL) may run long.",
		}
	}
	return []string{
		"What to do about it:",
		"- The room under the budget is a quota, and spending it is fine. What is " +
			"reported here is spending the LAST of it. Compressing prose -- your own or " +
			"the file's -- until the arithmetic passes is not a fix either: it shreds " +
			"detail and still leaves the file with nothing left.",
		"- Leaving no room is a TIME BOMB for the next agent, not a passing grade. A " +
			"file with nothing left breaks on the next edit of any size, and whoever " +
			"makes that edit inherits the reorganization you skipped. Being told the " +
			"file is too big makes the extraction yours, in this change.",
		"- Move depth OUT to docs/<topic>.md and leave a pointer: one line naming the " +
			"path and what is in it. Move the prose VERBATIM -- extraction, never " +
			"summarization; the detail has to survive somewhere it can grow.",
		"- NEVER add to an already-oversized item. Appending to the biggest bullet in " +
			"the file is how these files got this way.",
		"- Hard-wrap at " + strconv.Itoa(widthLimit) + " columns. An unwrapped file turns every edit into a " +
			"one-line diff nobody can review, and a paragraph that runs for thousands of " +
			"columns is the SHAPE of an item that should have been a pointer to docs/.",
		"- CLAUDE.md is an index, not a manual: what exists, the invariants one line " +
			"each, and where the depth lives.",
	}
}

func widthNote(o offender) string {
	if len(o.Wide) == 0 {
		return ""
	}
	shown := make([]string, 0, 5)
	for i, n := range o.Wide {
		if i == 5 {
			break
		}
		shown = append(shown, strconv.Itoa(n))
	}
	more := ""
	if len(o.Wide) > 5 {
		more = ", ..."
	}
	unit := " lines"
	if len(o.Wide) == 1 {
		unit = " line"
	}
	return "; " + strconv.Itoa(len(o.Wide)) + unit + " over " + strconv.Itoa(widthLimit) +
		" columns (" + strings.Join(shown, ", ") + more + ")"
}

func entry(o offender, limit int) string {
	switch {
	case o.Chars > limit:
		return fmt.Sprintf("  %s -- %s chars (%.2fx budget, %s over)%s",
			o.Path, comma(o.Chars), float64(o.Chars)/float64(limit), comma(o.Chars-limit), widthNote(o))
	case o.Chars < nearLimit(limit):
		// Under budget, but unwrapped: still worth naming, on its own terms.
		return fmt.Sprintf("  %s -- %s chars, within budget%s", o.Path, comma(o.Chars), widthNote(o))
	default:
		unit := " characters"
		if limit-o.Chars == 1 {
			unit = " character"
		}
		return fmt.Sprintf("  %s -- %s chars (%d%% of budget, only %s%s of room left)%s",
			o.Path, comma(o.Chars), int(float64(o.Chars)/float64(limit)*100+0.5),
			comma(limit-o.Chars), unit, widthNote(o))
	}
}

const unit = "(characters, the way the CLI counts them -- not bytes, so wc -c overstates " +
	"any file with non-ASCII text in it)"

// sessionReport is the census at session start.
func sessionReport(offenders []offender, limit int) string {
	var over, near []offender
	for _, o := range offenders {
		if o.Chars > limit {
			over = append(over, o)
		} else {
			near = append(near, o)
		}
	}

	var lines []string
	if len(over) > 0 {
		what := "one file"
		if len(over) != 1 {
			what = strconv.Itoa(len(over)) + " files"
		}
		lines = append(lines, "INSTRUCTION-FILE BUDGET EXCEEDED: "+what+
			" loaded into this session's context is over the "+comma(limit)+"-character budget "+unit+".")
	} else {
		what := "one file"
		if len(near) != 1 {
			what = strconv.Itoa(len(near)) + " files"
		}
		lines = append(lines, "INSTRUCTION FILE AT THE BUDGET WALL: "+what+
			" loaded into this session's context has effectively no room left under the "+
			comma(limit)+"-character budget "+unit+".")
	}

	lines = append(lines, "")
	for _, o := range over {
		lines = append(lines, entry(o, limit))
	}
	if len(over) > 0 && len(near) > 0 {
		lines = append(lines, "", "Under the budget, but with no room left -- one edit from the same problem:")
	}
	for _, o := range near {
		lines = append(lines, entry(o, limit))
	}

	lines = append(lines, "",
		"Nothing was truncated. That is the problem, not the reprieve: every byte above "+
			"is re-sent on EVERY request for the whole session, so an oversized instruction "+
			"file is a permanent tax on the context the actual task needs, and its rules "+
			"compete with each other for attention.",
		"")
	lines = append(lines, remedy()...)
	lines = append(lines,
		"- Editing one of these files this session? Then fix it as you go: extracting a "+
			"section into docs/ is in-scope work, not scope creep. If you are not touching "+
			"them, leave them be -- this is standing context for the next time you do.")
	return strings.Join(lines, "\n")
}

// editReportText is what a session hears immediately after writing an
// instruction file that is over budget, at the wall, or unwrapped.
func editReportText(o offender, limit int, growth int, hasGrowth bool) string {
	narrow := widthOnly(o, limit)

	headline := "INSTRUCTION-FILE BUDGET: the file you just wrote is at the " +
		comma(limit) + "-character budget wall."
	switch {
	case o.Chars > limit:
		headline = "INSTRUCTION-FILE BUDGET: the file you just wrote is OVER the " +
			comma(limit) + "-character budget."
	case narrow:
		// Say the true offense. The old headline claimed the budget wall for
		// every width-only report, on files with thousands of characters spare.
		headline = "INSTRUCTION-FILE WIDTH: the file you just wrote has lines past " +
			strconv.Itoa(widthLimit) + " columns. It is within budget."
	}

	lines := []string{headline, "", entry(o, limit)}
	if hasGrowth && growth > 0 {
		lines = append(lines, "", "  ...of which this session's uncommitted edits added "+comma(growth)+" characters.")
	}
	if narrow {
		lines = append(lines, "",
			"Wrap them before you finish. An unwrapped file turns every later edit into a "+
				"one-line diff nobody can review, so a two-word change and a rewritten "+
				"paragraph look identical.",
			"")
	} else {
		lines = append(lines, "",
			"Do not finish the change here. Every character above is re-sent on EVERY "+
				"request of every future session, and a file with no room left hands the next "+
				"agent a file that breaks on its next edit.",
			"")
	}
	lines = append(lines, remedyFor(narrow)...)
	return strings.Join(lines, "\n")
}

// stopReason is the refusal: a turn may not quietly end having left an
// instruction file this session wrote over budget, flush against it, or
// unwrapped.
func stopReason(still []offender, limit int) string {
	what := "an instruction file"
	if len(still) != 1 {
		what = strconv.Itoa(len(still)) + " instruction files"
	}
	narrow := allWidthOnly(still, limit)

	opener := "You are ending the turn having left " + what + " you wrote this session with no " +
		"room under the " + comma(limit) + "-character budget:"
	if narrow {
		opener = "You are ending the turn having left " + what + " you wrote this session with " +
			"lines past " + strconv.Itoa(widthLimit) + " columns. They are within budget:"
	}
	lines := []string{opener, ""}
	for _, o := range still {
		lines = append(lines, entry(o, limit))
	}
	if narrow {
		lines = append(lines, "",
			"Wrap them and finish. Nothing needs extracting -- this is reflow, and it is what "+
				"keeps the next edit to these files reviewable.",
			"")
		lines = append(lines, "If a line genuinely cannot be wrapped (a table row, a fenced block, one long "+
			"URL), say so to the user and name it.")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "",
		"Finish the job: move a section into docs/<topic>.md VERBATIM and leave a "+
			"one-line pointer, so the file comes back well under the budget. Trimming prose "+
			"until it fits, or landing it one character under the limit, is not an "+
			"acceptable resolution -- it destroys detail and hands the next agent a file "+
			"that breaks on its next edit.",
		"",
		"If extraction genuinely does not belong in this change, say so explicitly to the "+
			"user and name what you left oversized. This gate will not repeat itself for a "+
			"file you leave as it is -- but it fires again for any file you touch and still "+
			"leave broken, so silently editing around it is not a way past it.")
	return strings.Join(lines, "\n")
}

// worstFirst sorts by size descending: that is the one worth fixing.
func worstFirst(offenders []offender) {
	sort.SliceStable(offenders, func(i, j int) bool { return offenders[i].Chars > offenders[j].Chars })
}
