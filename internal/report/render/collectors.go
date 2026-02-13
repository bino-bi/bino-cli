package render

import (
"bytes"
"encoding/json"
"fmt"
"net/url"
"os"
"path/filepath"
"strings"

"bino.bi/bino/internal/report/config"
)

// fontAsset represents a font resource to be linked in the HTML.
type fontAsset struct {
	href      string
	mediaType string
}

// assetComponent represents a named asset to be rendered as bn-asset.
type assetComponent struct {
	name  string
	value string
}

// componentStyle represents a component style configuration.
type componentStyle struct {
	name  string
	value string
}

// internationalization represents a locale-specific i18n entry.
type internationalization struct {
	code      string
	namespace string
	value     string
}

// assetSpec defines the structure for Asset manifests.
type assetSpec struct {
	Type      string      `json:"type"`
	MediaType string      `json:"mediaType"`
	Source    assetSource `json:"source"`
}

// assetSource defines the source location for an asset.
type assetSource struct {
	InlineBase64 string `json:"inlineBase64"`
	LocalPath    string `json:"localPath"`
	RemoteURL    string `json:"remoteURL"`
}

// componentStyleSpec defines the structure for ComponentStyle manifests.
type componentStyleSpec struct {
	Content json.RawMessage `json:"content"`
}

func (s componentStyleSpec) normalizedContent() (string, error) {
	return normalizeSpecContent(s.Content, "component style")
}

// internationalizationSpec defines the structure for Internationalization manifests.
type internationalizationSpec struct {
	Code      string          `json:"code"`
	Namespace string          `json:"namespace"`
	Content   json.RawMessage `json:"content"`
}

func (s internationalizationSpec) normalizedContent() (string, error) {
	return normalizeSpecContent(s.Content, "internationalization")
}

// normalizeSpecContent parses JSON content that may be either a raw object or a JSON string.
func normalizeSpecContent(raw json.RawMessage, label string) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("%s content is required", label)
	}
	if trimmed[0] == '"' {
		var rawString string
		if err := json.Unmarshal(trimmed, &rawString); err != nil {
			return "", fmt.Errorf("%s string content: %w", label, err)
		}
		jsonString := strings.TrimSpace(rawString)
		if jsonString == "" {
			return "", fmt.Errorf("%s content cannot be empty", label)
		}
		if !json.Valid([]byte(jsonString)) {
			return "", fmt.Errorf("%s content must be valid JSON", label)
		}
		return jsonString, nil
	}
	if !json.Valid(trimmed) {
		return "", fmt.Errorf("%s content must be valid JSON", label)
	}
	return string(trimmed), nil
}

// collectInternationalizations extracts internationalization entries from documents.
func collectInternationalizations(docs []config.Document) ([]internationalization, error) {
	var entries []internationalization
	for _, doc := range docs {
		if doc.Kind != "Internationalization" {
			continue
		}
		var payload struct {
			Spec internationalizationSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("render: parse internationalization %s: %w", doc.Name, err)
		}
		if strings.TrimSpace(payload.Spec.Code) == "" {
			return nil, fmt.Errorf("render: internationalization %s: spec.code is required", doc.Name)
		}
		value, err := payload.Spec.normalizedContent()
		if err != nil {
			return nil, fmt.Errorf("render: internationalization %s: %w", doc.Name, err)
		}
		entries = append(entries, internationalization{
code:      payload.Spec.Code,
namespace: payload.Spec.Namespace,
value:     value,
})
	}
	return entries, nil
}

// collectComponentStyles extracts component style configurations from documents.
func collectComponentStyles(docs []config.Document) ([]componentStyle, error) {
	var styles []componentStyle
	for _, doc := range docs {
		if doc.Kind != "ComponentStyle" {
			continue
		}
		var payload struct {
			Spec componentStyleSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("render: parse component style %s: %w", doc.Name, err)
		}
		value, err := payload.Spec.normalizedContent()
		if err != nil {
			return nil, fmt.Errorf("render: component style %s: %w", doc.Name, err)
		}
		styles = append(styles, componentStyle{name: doc.Name, value: value})
	}
	return styles, nil
}

// collectAssets extracts font and file assets from documents.
func collectAssets(docs []config.Document) ([]fontAsset, []assetComponent, []LocalAsset, error) {
	var (
fonts  []fontAsset
assets []assetComponent
locals []LocalAsset
)
	for _, doc := range docs {
		if doc.Kind != "Asset" {
			continue
		}
		var payload struct {
			Spec assetSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, nil, nil, fmt.Errorf("render: parse asset %s: %w", doc.Name, err)
		}
		switch payload.Spec.Type {
		case "font":
			href, local, err := resolveAssetValue(doc, payload.Spec, fontURLPath)
			if err != nil {
				return nil, nil, nil, err
			}
			fonts = append(fonts, fontAsset{href: href, mediaType: payload.Spec.MediaType})
			if local != nil {
				locals = append(locals, *local)
			}
		default:
			value, local, err := resolveAssetValue(doc, payload.Spec, assetURLPath)
			if err != nil {
				return nil, nil, nil, err
			}
			assets = append(assets, assetComponent{name: doc.Name, value: value})
			if local != nil {
				locals = append(locals, *local)
			}
		}
	}
	return fonts, assets, locals, nil
}

// resolveAssetValue determines the URL or data URI for an asset.
func resolveAssetValue(doc config.Document, spec assetSpec, aliasFn func(string) string) (string, *LocalAsset, error) {
	source := spec.Source
	switch {
	case source.RemoteURL != "":
		return source.RemoteURL, nil, nil
	case source.InlineBase64 != "":
		if spec.MediaType == "" {
			return "", nil, fmt.Errorf("render: asset %s inline source requires mediaType", doc.Name)
		}
		return fmt.Sprintf("data:%s;base64,%s", spec.MediaType, source.InlineBase64), nil, nil
	case source.LocalPath != "":
		absPath, err := resolveLocalAssetPath(doc.File, source.LocalPath)
		if err != nil {
			return "", nil, fmt.Errorf("render: asset %s local path %s: %w", doc.Name, source.LocalPath, err)
		}
		alias := aliasFn(doc.Name)
		local := LocalAsset{
			URLPath:   alias,
			FilePath:  absPath,
			MediaType: spec.MediaType,
		}
		return alias, &local, nil
	default:
		return "", nil, fmt.Errorf("render: asset %s must define a source", doc.Name)
	}
}

// resolveLocalAssetPath resolves a local asset path relative to the document file.
func resolveLocalAssetPath(docFile, src string) (string, error) {
	resolved := src
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(docFile), src)
	}
	absPath, err := filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory", absPath)
	}
	return absPath, nil
}

// ResolveAssetURLs builds a name→URL map for all non-font Asset documents
// and returns the corresponding local assets that need HTTP serving.
func ResolveAssetURLs(docs []config.Document) (map[string]string, []LocalAsset, error) {
	urls := make(map[string]string)
	var locals []LocalAsset
	for _, doc := range docs {
		if doc.Kind != "Asset" {
			continue
		}
		var payload struct {
			Spec assetSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, nil, fmt.Errorf("render: parse asset %s: %w", doc.Name, err)
		}
		if payload.Spec.Type == "font" {
			continue
		}
		value, local, err := resolveAssetValue(doc, payload.Spec, assetURLPath)
		if err != nil {
			return nil, nil, err
		}
		urls[doc.Name] = value
		if local != nil {
			locals = append(locals, *local)
		}
	}
	return urls, locals, nil
}

// fontURLPath generates a URL path for font assets.
func fontURLPath(name string) string {
	return "/assets/fonts/" + url.PathEscape(name)
}

// assetURLPath generates a URL path for file assets.
func assetURLPath(name string) string {
	return "/assets/files/" + url.PathEscape(name)
}
