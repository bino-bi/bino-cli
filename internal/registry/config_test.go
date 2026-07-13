package registry

import "testing"

func TestResolveConfig(t *testing.T) {
	// Isolate from the developer's real ~/.bino/config.toml and environment.
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvURL, "")

	t.Run("defaults", func(t *testing.T) {
		cfg, err := ResolveConfig("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.URL != DefaultURL || cfg.Token != "" {
			t.Errorf("got %+v, want default URL and empty token", cfg)
		}
	})

	t.Run("trims trailing slash", func(t *testing.T) {
		cfg, err := ResolveConfig("http://127.0.0.1:8090/", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.URL != "http://127.0.0.1:8090" {
			t.Errorf("URL = %q", cfg.URL)
		}
	})

	t.Run("literal token", func(t *testing.T) {
		cfg, err := ResolveConfig("", "s3cret")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != "s3cret" {
			t.Errorf("Token = %q", cfg.Token)
		}
	})

	t.Run("env var expansion", func(t *testing.T) {
		t.Setenv("TEST_REG_TOKEN", "from-env")
		cfg, err := ResolveConfig("", "${TEST_REG_TOKEN}")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != "from-env" {
			t.Errorf("Token = %q", cfg.Token)
		}
	})

	t.Run("unset env var errors", func(t *testing.T) {
		if _, err := ResolveConfig("", "${DEFINITELY_UNSET_VAR_42}"); err == nil {
			t.Fatal("expected error for unset env var")
		}
	})

	t.Run("fallback env token", func(t *testing.T) {
		t.Setenv(EnvToken, "fallback-token")
		cfg, err := ResolveConfig("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Token != "fallback-token" {
			t.Errorf("Token = %q", cfg.Token)
		}
	})

	t.Run("env URL when raw URL empty", func(t *testing.T) {
		t.Setenv(EnvURL, "https://env.example.com/")
		cfg, err := ResolveConfig("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.URL != "https://env.example.com" {
			t.Errorf("URL = %q", cfg.URL)
		}
	})

	t.Run("explicit URL beats env", func(t *testing.T) {
		t.Setenv(EnvURL, "https://env.example.com")
		cfg, err := ResolveConfig("https://project.example.com", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.URL != "https://project.example.com" {
			t.Errorf("URL = %q", cfg.URL)
		}
	})

	t.Run("global config when env unset", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeGlobalConfig(t, "[registry]\nurl = \"https://global.example.com/\"\n")
		cfg, err := ResolveConfig("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.URL != "https://global.example.com" {
			t.Errorf("URL = %q", cfg.URL)
		}
	})

	t.Run("env beats global config", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeGlobalConfig(t, "[registry]\nurl = \"https://global.example.com\"\n")
		t.Setenv(EnvURL, "https://env.example.com")
		cfg, err := ResolveConfig("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.URL != "https://env.example.com" {
			t.Errorf("URL = %q", cfg.URL)
		}
	})

	t.Run("malformed global config errors", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		writeGlobalConfig(t, "[[registry\n")
		if _, err := ResolveConfig("", ""); err == nil {
			t.Fatal("expected error for malformed global config")
		}
	})
}
