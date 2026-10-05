// collate.go ports the path-ordering comparator the builtin used for mtime
// ties. That comparator is JS String.prototype.localeCompare. It is ICU
// collation under Node's default locale, which is en-US in practice. x/text/collate's root collation
// reproduces Node v22's en-US localeCompare sign for every pair in the
// committed vector set (collate_test.go). Punctuation classes order by
// collation weight (space < "_" < "-" < "." < "/"). Digits sort before
// letters. Letters compare case-insensitively at primary strength, and
// lowercase sorts first on ties. Accented letters sort right after their
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
