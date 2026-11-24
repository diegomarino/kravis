package storage

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden record files")

// testTask and testInteraction stand in for the record types of later
// increments; they only pin the disk format.
type testTask struct {
	Base
	Holder   string `json:"holder"`
	Title    string `json:"title"`
	Status   string `json:"status,omitempty"`
	Category string `json:"category,omitempty"`
	Live     bool   `json:"live"`
	Assignee string `json:"assignee,omitempty"`
	Agent    string `json:"agent,omitempty"`
	From     string `json:"from,omitempty"`
	Purpose  string `json:"purpose,omitempty"`
	Due      string `json:"due,omitempty"`
}

type testInteraction struct {
	Base
	Holder       string `json:"holder"`
	Subject      string `json:"subject"`
	Channel      string `json:"channel,omitempty"`
	Participants IDs    `json:"participants,omitempty"`
	Refs         IDs    `json:"refs,omitempty"`
	Live         bool   `json:"live"`
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func goldenTask() *testTask {
	return &testTask{
		Base: Base{
			ID: "tk_01jdx7v3k2f9q8w4e6r5t2y1u0", Version: 3,
			CreatedAt: at("2026-11-23T17:02:11Z"), CreatedBy: "us_correo-petalo",
			UpdatedAt: at("2026-11-24T09:15:40Z"), UpdatedBy: "us_diego-petalo",
		},
		Holder: "org_petalo", Title: "Send the teaser to Elena", Status: "todo", Category: "open", Live: true,
		Assignee: "us_diego-petalo", Agent: "us_correo-petalo", From: "it_2026-11-23-documentacion-data-room", //nolint:misspell // Spanish, as in docs/storage.md
		Purpose: "follow-up", Due: "2026-11-28",
	}
}

func goldenInteraction() *testInteraction {
	return &testInteraction{
		Base: Base{
			ID: "it_2026-11-23-documentacion-data-room", Version: 1, //nolint:misspell // Spanish
			CreatedAt: at("2026-11-23T17:00:00Z"), CreatedBy: "us_correo-petalo",
			UpdatedAt: at("2026-11-23T17:00:00Z"), UpdatedBy: "us_correo-petalo",
		},
		Holder:       "org_petalo",
		Subject:      "Documentación del data room <v2> & anexos",
		Participants: IDs{"pe_elena-ruiz", "pe_bruno-alba", "pe_ana-mora"},
		Live:         true,
	}
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	file := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.WriteFile(file, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("%s differs from the golden file:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

func TestRecordFormatMatchesGoldenFiles(t *testing.T) {
	for name, rec := range map[string]Record{"task.json": goldenTask(), "interaction.json": goldenInteraction()} {
		got, err := Encode(rec)
		if err != nil {
			t.Fatal(err)
		}
		checkGolden(t, name, got)
	}
}

func TestRecordFormatDetails(t *testing.T) {
	rec := goldenInteraction()
	got, err := Encode(rec)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	for _, want := range []string{
		"{\n  \"id\": ", // id first, two spaces
		"Documentación del data room <v2> & anexos", // UTF-8 and HTML unescaped
		"\"participants\": [\n    \"pe_ana-mora\",\n    \"pe_bruno-alba\",\n    \"pe_elena-ruiz\"\n  ],",
		"\"updated_by\": \"us_correo-petalo\"\n}\n", // metadata last, trailing newline
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	for _, absent := range []string{"channel", "refs", `\u`} {
		if strings.Contains(s, absent) {
			t.Errorf("%q should not appear in\n%s", absent, s)
		}
	}
	if rec.Participants[0] != "pe_elena-ruiz" {
		t.Error("encoding sorted the caller's list in place")
	}
}

func TestUnknownFieldsAreKeptOnRewrite(t *testing.T) {
	disk := `{
  "id": "tk_1",
  "holder": "org_petalo",
  "title": "Call Ana",
  "zeta": {"b": 1.50, "a": "é"},
  "live": true,
  "alpha": [3, 1],
  "version": 2,
  "created_at": "2026-11-23T17:02:11Z",
  "created_by": "us_a",
  "updated_at": "2026-11-23T17:02:11Z",
  "updated_by": "us_a"
}
`
	var rec testTask
	if err := Decode([]byte(disk), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Title != "Call Ana" || len(rec.Unknown) != 2 {
		t.Fatalf("decoded %+v", rec)
	}
	rec.Title = "Call Ana again"
	got, err := Encode(&rec)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "id": "tk_1",
  "holder": "org_petalo",
  "title": "Call Ana again",
  "live": true,
  "alpha": [
    3,
    1
  ],
  "zeta": {
    "a": "é",
    "b": 1.50
  },
  "version": 2,
  "created_at": "2026-11-23T17:02:11Z",
  "created_by": "us_a",
  "updated_at": "2026-11-23T17:02:11Z",
  "updated_by": "us_a"
}
`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestDecodeRoundTripsTheGoldenFiles(t *testing.T) {
	for name, rec := range map[string]Record{"task.json": &testTask{}, "interaction.json": &testInteraction{}} {
		data, err := os.ReadFile(filepath.Join("testdata", "golden", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := Decode(data, rec); err != nil {
			t.Fatal(err)
		}
		if rec.base().Unknown != nil {
			t.Errorf("%s: unknown fields %v", name, rec.base().Unknown)
		}
		got, _ := Encode(rec)
		if string(got) != string(data) {
			t.Errorf("%s does not round-trip:\n%s", name, got)
		}
	}
}

func TestDecodeRejectsBadRecords(t *testing.T) {
	for _, bad := range []string{`[]`, `{"title": 3}`, `not json`} {
		if err := Decode([]byte(bad), &testTask{}); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
	rec := &testTask{Base: Base{ID: "tk_1", Unknown: map[string]jsonRaw{"x": jsonRaw("{bad")}}}
	if _, err := Encode(rec); err == nil {
		t.Error("a broken unknown field must not be written")
	}
	// A defined field wins over an unknown one of the same name.
	rec = &testTask{Base: Base{ID: "tk_1", Unknown: map[string]jsonRaw{"title": jsonRaw(`"old"`)}}, Title: "new"}
	if got, _ := Encode(rec); strings.Count(string(got), "title") != 1 {
		t.Errorf("got %s", got)
	}
}

func TestDecodeStrictRefusesUnknownFields(t *testing.T) {
	var v struct {
		Title string `json:"title"`
	}
	if err := DecodeStrict(strings.NewReader(`{"title": "a"}`), &v); err != nil || v.Title != "a" {
		t.Fatalf("got %+v, %v", v, err)
	}
	for _, bad := range []string{`{"title": "a", "extra": 1}`, `{"title": "a"} {}`, `{`} {
		if err := DecodeStrict(strings.NewReader(bad), &v); err == nil {
			t.Errorf("%s: want an error", bad)
		}
	}
}

type jsonRaw = json.RawMessage
