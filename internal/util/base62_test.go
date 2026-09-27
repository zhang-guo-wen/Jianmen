package util

import (
	"testing"
)

func TestEncodeBase62(t *testing.T) {
	cases := []struct {
		n    uint64
		want string
	}{
		{0, "0"},
		{1, "1"},
		{9, "9"},
		{10, "a"},
		{35, "z"},
		{36, "A"},
		{61, "Z"},
		{62, "10"},
		{3844, "100"}, // 62*62 + 0
	}
	for _, c := range cases {
		got := EncodeBase62(c.n)
		if got != c.want {
			t.Errorf("EncodeBase62(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestDecodeBase62(t *testing.T) {
	cases := []struct {
		s    string
		want uint64
		ok   bool
	}{
		{"0", 0, true},
		{"9", 9, true},
		{"a", 10, true},
		{"z", 35, true},
		{"A", 36, true},
		{"Z", 61, true},
		{"10", 62, true},
		{"100", 3844, true},
		{"", 0, false},
		{"@", 0, false},
		{"lYGhA16ahyf", 18446744073709551615, true}, // math.MaxUint64
		{"lYGhA16ahyg", 0, false},                   // math.MaxUint64 + 1
	}
	for _, c := range cases {
		got, err := DecodeBase62(c.s)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("DecodeBase62(%q) = (%d, %v), want (%d, nil)", c.s, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("DecodeBase62(%q) should have failed", c.s)
		}
	}
}

func TestEncodeBase62Padded(t *testing.T) {
	cases := []struct {
		n     uint64
		width int
		want  string
	}{
		{0, 4, "0000"},
		{1, 4, "0001"},
		{62, 4, "0010"},
		{14776335, 4, "ZZZZ"}, // 62^4 - 1
	}
	for _, c := range cases {
		got := EncodeBase62Padded(c.n, c.width)
		if got != c.want {
			t.Errorf("EncodeBase62Padded(%d, %d) = %q, want %q", c.n, c.width, got, c.want)
		}
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	for _, n := range []uint64{0, 1, 61, 62, 100, 999, 14776335, 999999999} {
		s := EncodeBase62(n)
		got, err := DecodeBase62(s)
		if err != nil {
			t.Errorf("DecodeBase62(%q) failed: %v", s, err)
		}
		if got != n {
			t.Errorf("roundtrip: %d -> %q -> %d", n, s, got)
		}
	}
}
