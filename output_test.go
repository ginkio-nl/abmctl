package main

import "testing"

func TestDateOnly(t *testing.T) {
	for in, want := range map[string]string{
		"2018-10-26T15:02:19.483Z":  "2018-10-26",
		"2026-10-21T00:00:00Z":      "2026-10-21",
		"2024-01-24T23:30:00-02:00": "2024-01-25", // normalized to UTC
		"":                          "",
		"not a date":                "not a date",
	} {
		if got := dateOnly(in); got != want {
			t.Errorf("dateOnly(%q) = %q, want %q", in, got, want)
		}
	}
}
