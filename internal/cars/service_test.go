package cars

import "testing"

func TestNormalizeAndValidatePlate(t *testing.T) {
	valid := map[string]string{
		"ts09ea1234":    "TS09EA1234",
		"TS 09 EA 1234": "TS09EA1234",
		"ap-28-1234":    "AP281234",
		"KA01AB9999":    "KA01AB9999",
	}
	for in, want := range valid {
		got := NormalizePlate(in)
		if got != want {
			t.Fatalf("NormalizePlate(%q) = %q, want %q", in, got, want)
		}
		if err := validatePlate(got); err != nil {
			t.Fatalf("validatePlate(%q) unexpected error: %v", got, err)
		}
	}
	for _, bad := range []string{"", "1234", "TS09EA12", "TSXXEA1234", "TS09EA12345", "'; DROP TABLE cars;--"} {
		if err := validatePlate(NormalizePlate(bad)); err == nil {
			t.Fatalf("validatePlate(%q) expected error", bad)
		}
	}
}
