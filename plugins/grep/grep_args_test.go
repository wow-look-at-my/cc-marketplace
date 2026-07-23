package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseErr(t *testing.T, args string) string {
	t.Helper()
	_, rpcErr := parseGrepArgs(json.RawMessage(args))
	require.NotNil(t, rpcErr, "expected an invalid-params error for %s", args)
	assert.Equal(t, codeInvalidParams, rpcErr.Code)
	return rpcErr.Message
}

func parseOK(t *testing.T, args string) *grepArgs {
	t.Helper()
	a, rpcErr := parseGrepArgs(json.RawMessage(args))
	require.Nil(t, rpcErr, "unexpected error for %s: %v", args, rpcErr)
	return a
}

func TestParseArgsDefaults(t *testing.T) {
	a := parseOK(t, `{"pattern":"x"}`)
	assert.Equal(t, "x", a.pattern)
	assert.Equal(t, modeFilenamesWithMatches, a.mode)
	assert.True(t, a.lineNums)
	assert.False(t, a.ignoreCase)
	assert.False(t, a.multiline)
	assert.Nil(t, a.headLimit)
	assert.Zero(t, a.offset)
	assert.Nil(t, a.before)
	assert.Nil(t, a.after)
	assert.Nil(t, a.dashC)
	assert.Nil(t, a.context)
}

func TestParseArgsNumericStringCoercion(t *testing.T) {
	// zod-preprocess parity: numeric strings matching /^-?\d+(\.\d+)?$/
	// coerce; anything else is a type error.
	a := parseOK(t, `{"pattern":"x","-C":"2","head_limit":"10","offset":"-1","-B":"1.5"}`)
	require.NotNil(t, a.dashC)
	assert.Equal(t, 2.0, *a.dashC)
	require.NotNil(t, a.headLimit)
	assert.Equal(t, 10.0, *a.headLimit)
	assert.Equal(t, -1.0, a.offset)
	require.NotNil(t, a.before)
	assert.Equal(t, 1.5, *a.before)

	parseErr(t, `{"pattern":"x","-C":"abc"}`)
	parseErr(t, `{"pattern":"x","-C":true}`)
	parseErr(t, `{"pattern":"x","head_limit":"1e5"}`)
	parseErr(t, `{"pattern":"x","head_limit":".5"}`)
	parseErr(t, `{"pattern":"x","offset":null}`)
}

func TestParseArgsBoolStringCoercion(t *testing.T) {
	a := parseOK(t, `{"pattern":"x","-i":"true","-n":"false","multiline":"true"}`)
	assert.True(t, a.ignoreCase)
	assert.False(t, a.lineNums)
	assert.True(t, a.multiline)

	// Only the exact lowercase strings coerce (zod cL parity).
	parseErr(t, `{"pattern":"x","-n":"TRUE"}`)
	parseErr(t, `{"pattern":"x","-n":1}`)
	parseErr(t, `{"pattern":"x","-i":"yes"}`)
	parseErr(t, `{"pattern":"x","multiline":null}`)
}

func TestParseArgsOutputModeEnum(t *testing.T) {
	for _, m := range []string{"content", "filenames_with_matches", "filenames", "count"} {
		a := parseOK(t, fmt.Sprintf(`{"pattern":"x","output_mode":%q}`, m))
		assert.Equal(t, m, a.mode)
	}
	// The retired builtin name is deliberately NOT accepted — no alias.
	msg := parseErr(t, `{"pattern":"x","output_mode":"files_with_matches"}`)
	assert.Contains(t, msg, `must be one of`)
	parseErr(t, `{"pattern":"x","output_mode":"banana"}`)
	parseErr(t, `{"pattern":"x","output_mode":42}`)
}

func TestParseArgsRequiredAndStrictness(t *testing.T) {
	msg := parseErr(t, `{}`)
	assert.Contains(t, msg, "requires the pattern argument")
	parseErr(t, `null`)
	parseErr(t, `"str"`)
	parseErr(t, `{"pattern":42}`)
	parseErr(t, `{"pattern":null}`)
	parseErr(t, `{"pattern":"x","path":null}`)
	parseErr(t, `{"pattern":"x","path":[]}`)
	parseErr(t, `{"pattern":"x","glob":7}`)
	msg = parseErr(t, `{"pattern":"x","bogus":1}`)
	assert.Contains(t, msg, `"bogus"`)
	// The builtin's "type" parameter was removed (ambiguous name); it is
	// now just another unknown argument.
	msg = parseErr(t, `{"pattern":"x","type":"js"}`)
	assert.Contains(t, msg, `"type"`)
}

func TestBuildRgArgsOrder(t *testing.T) {
	base := []string{
		"--hidden",
		"--glob", "!.git", "--glob", "!.svn", "--glob", "!.hg",
		"--glob", "!.bzr", "--glob", "!.jj", "--glob", "!.sl",
		"--max-columns", "500",
	}
	// Content mode, everything set: multiline, -i, -n, context
	// precedence, dash pattern via -e, tokenized globs.
	a := parseOK(t, `{"pattern":"-dash","output_mode":"content","multiline":true,"-i":true,"context":2,"-C":1,"-B":9,"-A":9,"glob":"*.go,*.ts *.{a,b}"}`)
	want := append(append([]string{}, base...),
		"-U", "--multiline-dotall", "-i", "-n", "-C", "2",
		"-e", "-dash",
		"--glob", "*.go", "--glob", "*.ts", "--glob", "*.{a,b}")
	assert.Equal(t, want, buildRgArgs(a))

	// filenames mode: -l, no -n, context ignored.
	a = parseOK(t, `{"pattern":"p","output_mode":"filenames","-C":3}`)
	want = append(append([]string{}, base...), "-l", "p")
	assert.Equal(t, want, buildRgArgs(a))

	// count mode: -c -H.
	a = parseOK(t, `{"pattern":"p","output_mode":"count"}`)
	want = append(append([]string{}, base...), "-c", "-H", "p")
	assert.Equal(t, want, buildRgArgs(a))

	// fwm mode: --json, context translated, no -n even when true.
	a = parseOK(t, `{"pattern":"p","-B":1,"-A":2}`)
	want = append(append([]string{}, base...), "--json", "-B", "1", "-A", "2", "p")
	assert.Equal(t, want, buildRgArgs(a))

	// content without -n; -C fallback when context absent.
	a = parseOK(t, `{"pattern":"p","output_mode":"content","-n":false,"-C":4}`)
	want = append(append([]string{}, base...), "-C", "4", "p")
	assert.Equal(t, want, buildRgArgs(a))
}

func TestJSSliceUnits(t *testing.T) {
	items := []int{0, 1, 2, 3, 4}
	assert.Equal(t, []int{1, 2}, jsSlice(items, 1, 3))
	assert.Equal(t, []int{0, 1, 2, 3, 4}, jsSlice(items, 0, 99))
	assert.Empty(t, jsSlice(items, 9, 12))
	assert.Empty(t, jsSlice(items, 3, 1))             // end < start
	assert.Equal(t, []int{4}, jsSlice(items, -1, 99)) // negative from end
	assert.Equal(t, []int{0, 1}, jsSlice(items, -99, 2))
	assert.Equal(t, []int{0, 1}, jsSlice(items, 0, 2.9)) // trunc toward zero
	assert.Equal(t, []int{0, 1, 2}, jsSlice(items, 0, -2))
}

func TestPaginateUnits(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	lim := func(f float64) *float64 { return &f }

	out, applied := paginate(items, nil, 0)
	assert.Equal(t, items, out) // default 250 covers everything
	assert.Nil(t, applied)

	out, applied = paginate(items, lim(2), 0)
	assert.Equal(t, []string{"a", "b"}, out)
	require.NotNil(t, applied)
	assert.Equal(t, 2.0, *applied)

	// Exactly at the limit: not "applied".
	out, applied = paginate(items, lim(4), 0)
	assert.Equal(t, items, out)
	assert.Nil(t, applied)

	// 0 = unlimited, offset still honored.
	out, applied = paginate(items, lim(0), 1)
	assert.Equal(t, []string{"b", "c", "d"}, out)
	assert.Nil(t, applied)

	// Fractional limit truncates the window but reports itself verbatim.
	out, applied = paginate(items, lim(2.5), 0)
	assert.Equal(t, []string{"a", "b"}, out)
	require.NotNil(t, applied)
	assert.Equal(t, "2.5", jsNumString(*applied))

	out, _ = paginate(items, lim(2), 3)
	assert.Equal(t, []string{"d"}, out)

	out, applied = paginate(items, nil, 9)
	assert.Empty(t, out)
	assert.Nil(t, applied)
}

func TestPaginationNoteUnits(t *testing.T) {
	lim := 250.0
	assert.Equal(t, "", paginationNote(nil, 0))
	assert.Equal(t, "limit: 250", paginationNote(&lim, 0))
	assert.Equal(t, "limit: 250, offset: 3", paginationNote(&lim, 3))
	assert.Equal(t, "offset: 3", paginationNote(nil, 3))
	assert.Equal(t, "", paginationNote(nil, -1)) // negative offsets are not reported
}

func TestCoerceHelpersDirect(t *testing.T) {
	n, ok := coerceNumber(json.RawMessage(`3.25`))
	assert.True(t, ok)
	assert.Equal(t, 3.25, n)
	_, ok = coerceNumber(json.RawMessage(`"12.5"`))
	assert.True(t, ok)
	_, ok = coerceNumber(json.RawMessage(`"--3"`))
	assert.False(t, ok)
	_, ok = coerceNumber(json.RawMessage(`[1]`))
	assert.False(t, ok)

	b, ok := coerceBool(json.RawMessage(`false`))
	assert.True(t, ok)
	assert.False(t, b)
	b, ok = coerceBool(json.RawMessage(`"true"`))
	assert.True(t, ok)
	assert.True(t, b)
	_, ok = coerceBool(json.RawMessage(`0`))
	assert.False(t, ok)
}
