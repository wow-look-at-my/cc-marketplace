// collate.go ports the path-ordering comparator the builtin used for
// mtime ties: JS String.prototype.localeCompare, i.e. ICU collation
// under Node's default locale (en-US in practice). x/text/collate's
// root collation reproduces Node v22's en-US localeCompare sign for
// every pair in the committed vector set (collate_test.go): punctuation
// classes order by collation weight (space < "_" < "-" < "." < "/"),
// digits sort before letters, letters compare case-insensitively at
// primary strength with lowercase first on ties, and accented letters
// sort right after their base letter. Closest-effort caveat: exact
// localeCompare output depends on the user's ICU locale, which the
// builtin inherited from the environment; this comparator pins the
// root/en-US behavior.
//
// This file is tool-agnostic and copied verbatim between the grep and
// glob sibling plugins.
package main

import (
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// newPathCollator returns the localeCompare-equivalent collator. A
// Collator is not safe for concurrent use — callers create one per sort
// and use it from a single goroutine.
func newPathCollator() *collate.Collator {
	return collate.New(language.Und)
}
