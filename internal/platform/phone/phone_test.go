package phone

import "testing"

func TestNormalize(t *testing.T) {
	valid := map[string]string{
		"9876543210":          "+919876543210",
		"98765 43210":         "+919876543210",
		"098765-43210":        "+919876543210",
		"919876543210":        "+919876543210",
		"+91 98765 43210":     "+919876543210",
		"0091 9876543210":     "+919876543210",
		"+1 (415) 555-2671":   "+14155552671",
		"  +44 20 7946 0958 ": "+442079460958",
	}
	for in, want := range valid {
		got, ok := Normalize(in)
		if !ok || got != want {
			t.Fatalf("Normalize(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}

	for _, in := range []string{
		"", "abc", "12345", "5876543210", "+91 5876543210", "+0123456789", "98765432101234567",
		"9876-543-210x", "+", "++", "1234567890123456789012345678901234567890123",
	} {
		if got, ok := Normalize(in); ok {
			t.Fatalf("Normalize(%q) = %q, expected rejection", in, got)
		}
	}
}

func TestSameNumberNormalizesIdentically(t *testing.T) {
	a, _ := Normalize("98765 43210")
	b, _ := Normalize("+91-9876543210")
	c, _ := Normalize("09876543210")
	if a != b || b != c {
		t.Fatalf("variants differ: %q %q %q", a, b, c)
	}
}
