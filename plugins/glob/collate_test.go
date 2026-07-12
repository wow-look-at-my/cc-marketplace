// collate_test.go pins newPathCollator to golden vectors generated
// with Node v22.22.2 (Intl default locale en-US):
//
//	names.sort((a, b) => a.localeCompare(b))   // -> collateSortedGolden
//	Math.sign(a.localeCompare(b))              // -> collateSignGolden
//
// The generator lives in the session notes; the vectors are frozen
// here so the test needs no node at run time. The set deliberately
// mixes case, digits, punctuation classes, accents, non-latin
// scripts, an astral-plane emoji, and path-shaped strings, and
// contains no two distinct strings that collate equal (ties would
// make the golden order depend on sort stability).
package main

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var collateNames = []string{
	"a.txt",
	"A.txt",
	"b.txt",
	"B.txt",
	"ab.txt",
	"Ab.txt",
	"AB.txt",
	"aB.txt",
	"aa.txt",
	"z.txt",
	"Z.txt",
	"zz.txt",
	"ZZ.txt",
	"1a.txt",
	"2.txt",
	"02.txt",
	"10.txt",
	"a1.txt",
	"A1.txt",
	"file2.go",
	"file10.go",
	"File2.go",
	"FILE10.go",
	"a-b.txt",
	"a_b.txt",
	"a b.txt",
	"a.b.txt",
	"-dash.txt",
	"_under.txt",
	".dot.txt",
	" space.txt",
	"ab-.txt",
	"e.txt",
	"E.txt",
	"\u00e9.txt",
	"\u00c9.txt",
	"\u00ea.txt",
	"f.txt",
	"\u03b1.txt",
	"\u03a9.txt",
	"\u0444.txt",
	"\u65e5\u672c\u8a9e.txt",
	"\U0001f600.txt",
	"src/main.go",
	"src2/main.go",
	"src/Main.go",
	"SRC/x.go",
	"a/b.txt",
	"a-b/c.txt",
	"a/b/c.txt",
	"/tmp/x/a.txt",
	"/tmp/x/B.txt",
	"/tmp/X/c.txt",
	"/tmp/x-1/d.txt",
}

var collateSortedGolden = []string{
	" space.txt",
	"_under.txt",
	"-dash.txt",
	".dot.txt",
	"/tmp/x-1/d.txt",
	"/tmp/x/a.txt",
	"/tmp/x/B.txt",
	"/tmp/X/c.txt",
	"\U0001f600.txt",
	"02.txt",
	"10.txt",
	"1a.txt",
	"2.txt",
	"a b.txt",
	"a_b.txt",
	"a-b.txt",
	"a-b/c.txt",
	"a.b.txt",
	"a.txt",
	"A.txt",
	"a/b.txt",
	"a/b/c.txt",
	"a1.txt",
	"A1.txt",
	"aa.txt",
	"ab-.txt",
	"ab.txt",
	"aB.txt",
	"Ab.txt",
	"AB.txt",
	"b.txt",
	"B.txt",
	"e.txt",
	"E.txt",
	"\u00e9.txt",
	"\u00c9.txt",
	"\u00ea.txt",
	"f.txt",
	"file10.go",
	"FILE10.go",
	"file2.go",
	"File2.go",
	"src/main.go",
	"src/Main.go",
	"SRC/x.go",
	"src2/main.go",
	"z.txt",
	"Z.txt",
	"zz.txt",
	"ZZ.txt",
	"\u03b1.txt",
	"\u03a9.txt",
	"\u0444.txt",
	"\u65e5\u672c\u8a9e.txt",
}

// collateSignGolden[i][j] is the sign of
// collateNames[i].localeCompare(collateNames[j]).
var collateSignGolden = []string{
	"=<<<<<<<<<<<<>>>><<<<<<>>>>>>>><<<<<<<<<<<><<<<<><>>>>",
	">=<<<<<<<<<<<>>>><<<<<<>>>>>>>><<<<<<<<<<<><<<<<><>>>>",
	">>=<>>>>><<<<>>>>>><<<<>>>>>>>>><<<<<<<<<<><<<<>>>>>>>",
	">>>=>>>>><<<<>>>>>><<<<>>>>>>>>><<<<<<<<<<><<<<>>>>>>>",
	">><<=<<<><<<<>>>>>><<<<>>>>>>>>><<<<<<<<<<><<<<>>>>>>>",
	">><<>=<>><<<<>>>>>><<<<>>>>>>>>><<<<<<<<<<><<<<>>>>>>>",
	">><<>>=>><<<<>>>>>><<<<>>>>>>>>><<<<<<<<<<><<<<>>>>>>>",
	">><<><<=><<<<>>>>>><<<<>>>>>>>>><<<<<<<<<<><<<<>>>>>>>",
	">><<<<<<=<<<<>>>>>><<<<>>>>>>>><<<<<<<<<<<><<<<>>>>>>>",
	">>>>>>>>>=<<<>>>>>>>>>>>>>>>>>>>>>>>>><<<<>>>>>>>>>>>>",
	">>>>>>>>>>=<<>>>>>>>>>>>>>>>>>>>>>>>>><<<<>>>>>>>>>>>>",
	">>>>>>>>>>>=<>>>>>>>>>>>>>>>>>>>>>>>>><<<<>>>>>>>>>>>>",
	">>>>>>>>>>>>=>>>>>>>>>>>>>>>>>>>>>>>>><<<<>>>>>>>>>>>>",
	"<<<<<<<<<<<<<=<>><<<<<<<<<<>>>><<<<<<<<<<<><<<<<<<>>>>",
	"<<<<<<<<<<<<<>=>><<<<<<<<<<>>>><<<<<<<<<<<><<<<<<<>>>>",
	"<<<<<<<<<<<<<<<=<<<<<<<<<<<>>>><<<<<<<<<<<><<<<<<<>>>>",
	"<<<<<<<<<<<<<<<>=<<<<<<<<<<>>>><<<<<<<<<<<><<<<<<<>>>>",
	">><<<<<<<<<<<>>>>=<<<<<>>>>>>>><<<<<<<<<<<><<<<>>>>>>>",
	">><<<<<<<<<<<>>>>>=<<<<>>>>>>>><<<<<<<<<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>>=><>>>>>>>>>>>>>>>><<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><=<<>>>>>>>>>>>>>>><<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>>>>=>>>>>>>>>>>>>>>><<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><><=>>>>>>>>>>>>>>><<<<><<<<>>>>>>>",
	"<<<<<<<<<<<<<>>>><<<<<<=>><>>>><<<<<<<<<<<><<<<<<<>>>>",
	"<<<<<<<<<<<<<>>>><<<<<<<=><>>>><<<<<<<<<<<><<<<<<<>>>>",
	"<<<<<<<<<<<<<>>>><<<<<<<<=<>>>><<<<<<<<<<<><<<<<<<>>>>",
	"<<<<<<<<<<<<<>>>><<<<<<>>>=>>>><<<<<<<<<<<><<<<<><>>>>",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<=><><<<<<<<<<<<<<<<<<<<<<<<",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<<=<><<<<<<<<<<<<<<<<<<<<<<<",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<>>=><<<<<<<<<<<<<<<<<<<<<<<",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<=<<<<<<<<<<<<<<<<<<<<<<<",
	">><<<<<<><<<<>>>>>><<<<>>>>>>>>=<<<<<<<<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><<<<>>>>>>>>>=<<<<<<<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><<<<>>>>>>>>>>=<<<<<<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><<<<>>>>>>>>>>>=<<<<<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><<<<>>>>>>>>>>>>=<<<<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><<<<>>>>>>>>>>>>>=<<<<<><<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>><<<<>>>>>>>>>>>>>>=<<<<><<<<>>>>>>>",
	">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>=<<<>>>>>>>>>>>>",
	">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>=<<>>>>>>>>>>>>",
	">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>=<>>>>>>>>>>>>",
	">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>=>>>>>>>>>>>>",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<>>>><<<<<<<<<<<=<<<<<<<>>>>",
	">>>>>>>>><<<<>>>>>>>>>>>>>>>>>>>>>>>>><<<<>=<<<>>>>>>>",
	">>>>>>>>><<<<>>>>>>>>>>>>>>>>>>>>>>>>><<<<>>=>>>>>>>>>",
	">>>>>>>>><<<<>>>>>>>>>>>>>>>>>>>>>>>>><<<<>><=<>>>>>>>",
	">>>>>>>>><<<<>>>>>>>>>>>>>>>>>>>>>>>>><<<<>><>=>>>>>>>",
	">><<<<<<<<<<<>>>><<<<<<>>>>>>>><<<<<<<<<<<><<<<=><>>>>",
	"<<<<<<<<<<<<<>>>><<<<<<>>><>>>><<<<<<<<<<<><<<<<=<>>>>",
	">><<<<<<<<<<<>>>><<<<<<>>>>>>>><<<<<<<<<<<><<<<>>=>>>>",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<>>>><<<<<<<<<<<<<<<<<<<=<<>",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<>>>><<<<<<<<<<<<<<<<<<<>=<>",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<>>>><<<<<<<<<<<<<<<<<<<>>=>",
	"<<<<<<<<<<<<<<<<<<<<<<<<<<<>>>><<<<<<<<<<<<<<<<<<<<<<=",
}

func TestCollatorMatchesLocaleCompareSigns(t *testing.T) {
	require.Equal(t, len(collateNames), len(collateSignGolden))

	col := newPathCollator()
	for i, a := range collateNames {
		row := collateSignGolden[i]
		require.Equal(t, len(collateNames), len(row))

		for j, b := range collateNames {
			want := 0
			switch row[j] {
			case '<':
				want = -1
			case '>':
				want = 1
			}
			got := col.CompareString(a, b)
			if got > 0 {
				got = 1
			} else if got < 0 {
				got = -1
			}
			assert.Equal(t, want, got, "localeCompare(%q, %q)", a, b)

		}
	}
}

func TestCollatorSortsLikeArraySort(t *testing.T) {
	got := append([]string(nil), collateNames...)
	col := newPathCollator()
	sort.SliceStable(got, func(i, j int) bool { return col.CompareString(got[i], got[j]) < 0 })
	assert.Equal(t, collateSortedGolden, got)

}
