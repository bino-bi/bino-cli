package cli

import (
	"strings"
	"testing"
)

func TestBuildAssetDocument(t *testing.T) {
	t.Run("local image asset", func(t *testing.T) {
		data := AssetManifestData{
			Name:        "company_logo",
			Description: "The company logo",
			Type:        AssetTypeImage,
			MediaType:   "image/png",
			LocalPath:   "assets/logo.png",
		}
		got := wizardRoundTrip(t, buildAssetDocument(data), "asset.yaml")
		assertContainsAll(t, got, []string{
			"kind: Asset",
			"name: company_logo",
			"description: The company logo",
			"type: image",
			"mediaType: image/png",
			"localPath: assets/logo.png",
		})
		if strings.Contains(got, "remoteURL") {
			t.Errorf("local asset must not carry a remoteURL:\n%s", got)
		}
	})

	t.Run("remote font asset", func(t *testing.T) {
		data := AssetManifestData{
			Name:      "custom_font",
			Type:      AssetTypeFont,
			MediaType: "font/ttf",
			RemoteURL: "https://fonts.example.com/roboto.ttf",
		}
		got := wizardRoundTrip(t, buildAssetDocument(data), "asset_font.yaml")
		assertContainsAll(t, got, []string{
			"type: font",
			"mediaType: font/ttf",
			"remoteURL: https://fonts.example.com/roboto.ttf",
		})
	})

	// The schema requires spec.source, so a sourceless Asset (which the
	// wizard prevents by requiring --path or --url) is rejected by the
	// validating write path.
	t.Run("sourceless asset is rejected at write time", func(t *testing.T) {
		data := AssetManifestData{
			Name:      "placeholder_file",
			Type:      AssetTypeFile,
			MediaType: "application/pdf",
		}
		err := WriteSchemaDocument(buildAssetDocument(data), t.TempDir(), "asset_nosource.yaml", false, discardCmd().OutOrStdout())
		if err == nil {
			t.Fatal("expected a schema validation error for the missing source")
		}
	})
}

func TestDetectMediaType(t *testing.T) {
	tests := []struct {
		name string
		data AssetManifestData
		want string
	}{
		{"png local path", AssetManifestData{LocalPath: "assets/logo.png"}, "image/png"},
		{"jpg", AssetManifestData{LocalPath: "photo.jpg"}, "image/jpeg"},
		{"jpeg", AssetManifestData{LocalPath: "photo.jpeg"}, "image/jpeg"},
		{"svg", AssetManifestData{LocalPath: "icon.svg"}, "image/svg+xml"},
		{"gif", AssetManifestData{LocalPath: "anim.gif"}, "image/gif"},
		{"webp", AssetManifestData{LocalPath: "img.webp"}, "image/webp"},
		{"ttf", AssetManifestData{LocalPath: "font.ttf"}, "font/ttf"},
		{"otf", AssetManifestData{LocalPath: "font.otf"}, "font/otf"},
		{"woff", AssetManifestData{LocalPath: "font.woff"}, "font/woff"},
		{"woff2", AssetManifestData{LocalPath: "font.woff2"}, "font/woff2"},
		{"pdf", AssetManifestData{LocalPath: "doc.pdf"}, "application/pdf"},
		{"uppercase extension", AssetManifestData{LocalPath: "LOGO.PNG"}, "image/png"},
		{"remote URL is used when no local path", AssetManifestData{RemoteURL: "https://cdn.example.com/pic.png"}, "image/png"},
		{"local path wins over remote URL", AssetManifestData{LocalPath: "a.svg", RemoteURL: "https://x/y.png"}, "image/svg+xml"},
		{"unknown extension", AssetManifestData{LocalPath: "data.bin"}, ""},
		{"no source at all", AssetManifestData{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectMediaType(tt.data); got != tt.want {
				t.Errorf("detectMediaType(%+v) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}
