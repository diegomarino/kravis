package domain

import "testing"

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
