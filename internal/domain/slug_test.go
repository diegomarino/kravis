package domain

import (
	"regexp"
	"sync"
	"testing"
	"unicode"
)

func TestSlug(t *testing.T) {
	for name, want := range map[string]string{
		"Elena Ruiz":                "elena-ruiz",
		"Revisión del Contrato":     "revision-del-contrato",
		"José Peña Núñez":           "jose-pena-nunez",
		"Françoise Lefèvre-Dubois":  "francoise-lefevre-dubois",
		"Søren Ødegård":             "soren-odegard",
		"Straße & Æsir, Łódź":       "strasse-aesir-lodz",
		"Đorđe Þór":                 "dorde-thor",
		"  --Grupo Sierra, S.L.-- ": "grupo-sierra-s-l",
		"Mötley Crüe 2026":          "motley-crue-2026",
		"Ελένη Παπαδοπούλου":        "",
		"東京 Holdings":               "holdings",
		"Алексей":                   "",
		"":                          "",
	} {
		if got := Slug(name); got != want {
			t.Errorf("Slug(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSlugOrdersNames(t *testing.T) {
	// Name ordering compares slugs, so accents and case do not move a name.
	if Slug("Álvaro") >= Slug("Bruno") || Slug("bruno") != Slug("Bruno") || Slug("Éva") >= Slug("Fabio") {
		t.Fatal("slugs do not order names alphabetically")
	}
}

func TestSlugIsSafeToCallInParallel(t *testing.T) {
	var wg sync.WaitGroup
	for _, name := range []string{"José", "Peña", "Ñandú", "Ærø"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = Slug(name)
			}
		}()
	}
	wg.Wait()
}

// The schema's pattern for names that become slugs matches a character
// exactly when Slug makes something of it.
func TestSlugPatternIsSlug(t *testing.T) {
	class := regexp.MustCompile("^" + SlugPattern + "$")
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if s := string(r); class.MatchString(s) != (Slug(s) != "") {
			t.Errorf("%U %q: the pattern matches it %v, Slug gives %q", r, s, class.MatchString(s), Slug(s))
		}
	}
}
