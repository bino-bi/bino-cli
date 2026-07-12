package registry

import (
	"os"
	"testing"
)

func TestCredentialsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	creds, err := LoadCredentials()
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if len(creds.Registries) != 0 {
		t.Fatalf("expected empty store, got %+v", creds)
	}
	creds.Set("https://registry.bino.bi/", Credential{PAT: "bino_pat_x", PATID: "abc", User: "a@b.c"})
	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("save: %v", err)
	}

	path, _ := CredentialsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("credentials file mode = %o, want 0600", info.Mode().Perm())
	}

	loaded, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	// Lookup normalizes, so a differently-written URL finds the entry.
	cred, ok := loaded.Get("HTTPS://registry.bino.bi:443")
	if !ok || cred.PAT != "bino_pat_x" || cred.PATID != "abc" {
		t.Errorf("Get = %+v, %v", cred, ok)
	}
	if !loaded.Delete("https://registry.bino.bi") || loaded.Delete("https://registry.bino.bi") {
		t.Error("Delete should report existence exactly once")
	}
}

func TestNormalizeRegistryURL(t *testing.T) {
	cases := map[string]string{
		"https://Registry.Bino.bi/":    "https://registry.bino.bi",
		"https://registry.bino.bi:443": "https://registry.bino.bi",
		"http://127.0.0.1:8090/":       "http://127.0.0.1:8090",
		"http://localhost:80":          "http://localhost",
		"https://reg.example.com/sub/": "https://reg.example.com/sub",
		"  https://reg.example.com  ":  "https://reg.example.com",
		"registry.bino.bi/":            "registry.bino.bi",
	}
	for in, want := range cases {
		if got := NormalizeRegistryURL(in); got != want {
			t.Errorf("NormalizeRegistryURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveConfigStoredCredentialFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvToken, "")
	creds := &Credentials{Registries: map[string]Credential{}}
	creds.Set("http://127.0.0.1:9999", Credential{PAT: "bino_pat_stored"})
	if err := SaveCredentials(creds); err != nil {
		t.Fatal(err)
	}

	cfg, err := ResolveConfig("http://127.0.0.1:9999/", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "bino_pat_stored" {
		t.Errorf("Token = %q, want stored PAT", cfg.Token)
	}

	// Explicit token wins over the stored credential.
	cfg, err = ResolveConfig("http://127.0.0.1:9999", "explicit-jwt")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "explicit-jwt" {
		t.Errorf("Token = %q, want explicit token", cfg.Token)
	}
}
