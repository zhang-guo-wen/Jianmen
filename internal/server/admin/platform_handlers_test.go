package admin

import "testing"

func TestPlatformAccountModelDefaultsPlatformNameToURL(t *testing.T) {
	account := platformAccountModel(platformAccountPayload{URL: "https://git.example.com"}, "owner-1")
	if account.PlatformName != account.URL {
		t.Fatalf("platform name = %q, want URL %q", account.PlatformName, account.URL)
	}
}

func TestPlatformAccountModelKeepsOptionalCustomPlatformName(t *testing.T) {
	account := platformAccountModel(platformAccountPayload{URL: "https://git.example.com", PlatformName: "????"}, "owner-1")
	if account.PlatformName != "????" {
		t.Fatalf("platform name = %q, want custom name", account.PlatformName)
	}
}
