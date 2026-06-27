package util

import "testing"

func TestResourceIDFromSeq(t *testing.T) {
	cases := []struct {
		prefix string
		seq    int
		want   string
	}{
		{PrefixHost, 0, "H0000"},
		{PrefixHost, 1, "H0001"},
		{PrefixDatabase, 0, "D0000"},
	}
	for _, c := range cases {
		got := ResourceIDFromSeq(c.prefix, c.seq)
		if got != c.want {
			t.Errorf("ResourceIDFromSeq(%q, %d) = %q, want %q", c.prefix, c.seq, got, c.want)
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
