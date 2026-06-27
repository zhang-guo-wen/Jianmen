package util

import "testing"

func TestResourceIDFromSeq(t *testing.T) {
	cases := []struct {
		prefix string
		seq    int
		want   string
	}{
		{PrefixHost, 0, "0000"},
		{PrefixHost, 1, "0001"},
		{PrefixDatabase, 0, "0000"},
	}
	for _, c := range cases {
		got := ResourceIDFromSeq(c.prefix, c.seq)
		if got != c.want {
			t.Errorf("ResourceIDFromSeq(%q, %d) = %q, want %q", c.prefix, c.seq, got, c.want)
		}
	}
}

func TestFullUsername(t *testing.T) {
	cases := []struct {
		prefix       string
		resourceSeq  int
		sessionSeq   int
		want         string
	}{
		{PrefixHost, 1, 1, "H000100001"},
		{PrefixDatabase, 0, 0, "D000000000"},
	}
	for _, c := range cases {
		got := FullUsername(c.prefix, c.resourceSeq, c.sessionSeq)
		if got != c.want {
			t.Errorf("FullUsername(%q, %d, %d) = %q, want %q", c.prefix, c.resourceSeq, c.sessionSeq, got, c.want)
		}
		// Verify round-trip
		prefix, rSeq, sSeq, err := ParseCompactUsername(got)
		if err != nil {
			t.Errorf("ParseCompactUsername(%q) failed: %v", got, err)
		}
		if prefix != c.prefix || int(rSeq) != c.resourceSeq || int(sSeq) != c.sessionSeq {
			t.Errorf("round-trip: %q -> prefix=%q rSeq=%d sSeq=%d", got, prefix, rSeq, sSeq)
		}
	}
}

func TestParseCompactUsername(t *testing.T) {
	prefix, rSeq, sSeq, err := ParseCompactUsername("H000100001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prefix != "H" || rSeq != 1 || sSeq != 1 {
		t.Errorf("got prefix=%q rSeq=%d sSeq=%d, want H/1/1", prefix, rSeq, sSeq)
	}
	_, _, _, err = ParseCompactUsername("short")
	if err == nil {
		t.Error("expected error for short username")
	}
}
