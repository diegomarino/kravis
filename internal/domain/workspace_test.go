package domain

import "testing"

func TestConfigAndMarkerValidation(t *testing.T) {
	good := Config{Timezone: "Europe/Madrid", Catalogs: DefaultCatalogs()}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]Config{
		"zone":     {Timezone: "Mars/Base"},
		"local":    {Timezone: "Local"},
		"repeated": {Timezone: "UTC", Catalogs: Catalogs{TaskStatuses: []Status{{"a", "open"}, {"a", "done"}}}},
		"category": {Timezone: "UTC", Catalogs: Catalogs{TaskStatuses: []Status{{"a", "nope"}}}},
	} {
		if c.Validate() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if (Marker{"ws_01abc", 1}).Validate() != nil {
		t.Error("a valid marker")
	}
	for name, m := range map[string]Marker{"id": {"x_1", 1}, "empty": {"ws_", 1}, "version": {"ws_1", 2}} {
		if m.Validate() == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
