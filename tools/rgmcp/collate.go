// collate.go ports the path-ordering comparator. The builtin used for mtime
// ties: JS String.prototype.localeCompare, i.e. ICU collation under Node's
// default locale (en-US in practice). x/text/collate's root collation
// reproduces Node v22's en-US localeCompare sign for every pair in the
// committed vector set (collate_test.go). Punctuation classes order by
// collation weight (space < "_" < "-" < "." < "/"), digits sort before
// letters, letters compare case-insensitively at primary strength with
// lowercase earliest on ties. And accented letters sort right after their
// base letter. Closest-effort caveat: exact localeCompare output depends on
// the user's ICU locale, which the builtin inherited from the environment.
// This comparator pins the root/en-US behavior.
package rgmcp

import (
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// NewPathCollator returns the localeCompare-equivalent collator.
func NewPathCollator() *collate.Collator {
	return collate.New(language.Und)
}
