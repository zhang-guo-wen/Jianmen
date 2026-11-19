package version

import "testing"

func TestDisplayNormalizesVersion(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	cases := []struct {
		raw  string
		want string
	}{
		{raw: "1.0.0", want: "1.0.0"},
		{raw: "v1.2.3", want: "1.2.3"},
		{raw: " V1.2.3 ", want: "1.2.3"},
		{raw: "", want: "dev"},
		{raw: "   ", want: "dev"},
	}
	for _, testCase := range cases {
		Version = testCase.raw
		if got := Display(); got != testCase.want {
			t.Fatalf("Display() with Version=%q = %q, want %q", testCase.raw, got, testCase.want)
		}
	}
}
