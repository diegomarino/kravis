// Package domain holds Kravis's records and rules.
package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// letters are Latin letters that do not decompose into a base letter and an
// accent.
var letters = strings.NewReplacer(
	"ß", "ss", "æ", "ae", "œ", "oe", "ø", "o", "ł", "l", "đ", "d", "ð", "d", "þ", "th", "ı", "i",
)

// Slug turns a name into lowercase ASCII with hyphens, as IDs and name
// ordering use it: accents are removed ("Peña" gives "pena"), and every run of
// anything else becomes one hyphen, never at either end. Letters with no
// ASCII form, such as Greek or Han, are dropped, so a name in such a script
// gives "" and the caller needs another source for the slug.
func Slug(name string) string {
	var b strings.Builder
	s := fold(name)
	hyphen := false
	for _, r := range s {
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			hyphen = false
			continue
		}
		hyphen = true
	}
	return b.String()
}

// fold lowercases s and removes its accents, as Slug does before it keeps
// only ASCII letters and digits; search compares text folded this way.
func fold(s string) string {
	// A chain keeps state, so each call builds its own.
	stripMarks := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ = transform.String(stripMarks, strings.ToLower(s))
	return letters.Replace(s)
}

// SlugPattern is a JSON Schema pattern that matches exactly the names Slug
// makes a slug from: those with at least one character that has an ASCII
// letter or digit form. TestSlugPatternIsSlug checks it against Slug for
// every character.
const SlugPattern = "[0-9A-Za-zÀ-ÖØ-öø-ĥĨ-ıĴ-ķĹ-ľŁ-ňŌ-ťŨ-žƠơƯưǍ-ǜǞ-ǣǦ-ǭǰǴǵǸ-țȞȟȦ-ȳḀ-ẙẞẠ-ỹKÅ]"
