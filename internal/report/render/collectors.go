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
	reportspec "bino.bi/bino/internal/report/spec"
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

// ruleSet represents an IBCS rule-set configuration.
type ruleSet struct {
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

// ruleSetSpec defines the structure for RuleSet manifests.
type ruleSetSpec struct {
	Content json.RawMessage `json:"content"`
}

func (s ruleSetSpec) normalizedContent() (string, error) {
	return normalizeSpecContent(s.Content, "rule set")
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

// collectInternationalizations extracts internationalization entries from
// documents for the artefact language. A caption bundle synthesized from the
// DataSets' derive/assert declarations comes first, so that a project-authored
// "_system" bundle for the same code, emitted after it, wins in the engine.
func collectInternationalizations(docs []config.Document, locale string) ([]internationalization, error) {
	var entries []internationalization
	if caption := derivedCaptionEntry(docs, locale); caption != nil {
		entries = append(entries, *caption)
	}
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
		namespace := strings.TrimSpace(payload.Spec.Namespace)
		if namespace == "" {
			// The engine reads the "_system" namespace by default; an entry
			// without a namespace attribute would otherwise be unreachable.
			namespace = "_system"
		}
		entries = append(entries, internationalization{
			code:      payload.Spec.Code,
			namespace: namespace,
			value:     value,
		})
	}
	return entries, nil
}

// derivedCaptionEntry builds the "_system" bundle that captions previous-period
// slots declared with a non-year shift as "PP". The engine's built-in label for
// pp1..pp4 is "PY", which is only right for a year shift. Nil when no DataSet
// declares such a slot, so projects without derive/assert render unchanged.
func derivedCaptionEntry(docs []config.Document, locale string) *internationalization {
	labels := map[string]string{}
	for _, doc := range docs {
		if doc.Kind != "DataSet" {
			continue
		}
		var payload struct {
			Spec struct {
				Derive map[string]reportspec.ShiftDeclaration `json:"derive"`
				Assert map[string]reportspec.ShiftDeclaration `json:"assert"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			// The executor reports a broken DataSet; the caption is not the place.
			continue
		}
		for _, decls := range []map[string]reportspec.ShiftDeclaration{payload.Spec.Derive, payload.Spec.Assert} {
			for slot, decl := range decls {
				fields := strings.Fields(decl.Shift)
				if len(fields) == 0 || fields[len(fields)-1] == "year" {
					continue
				}
				labels["global."+slot] = "PP"
			}
		}
	}
	if len(labels) == 0 {
		return nil
	}
	value, err := json.Marshal(labels)
	if err != nil {
		return nil
	}
	return &internationalization{code: locale, namespace: "_system", value: string(value)}
}

// scalingGroup represents a named scaling value for synchronized chart/table scaling.
type scalingGroup struct {
	name  string
	value float64
}

// scalingGroupSpec defines the structure for ScalingGroup manifests.
type scalingGroupSpec struct {
	Value float64 `json:"value"`
}

// collectScalingGroups extracts scaling group entries from documents.
func collectScalingGroups(docs []config.Document) ([]scalingGroup, error) {
	var groups []scalingGroup
	for _, doc := range docs {
		if doc.Kind != "ScalingGroup" {
			continue
		}
		var payload struct {
			Spec scalingGroupSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("render: parse scaling group %s: %w", doc.Name, err)
		}
		groups = append(groups, scalingGroup{name: doc.Name, value: payload.Spec.Value})
	}
	return groups, nil
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

// collectRuleSets extracts rule-set configurations from documents.
func collectRuleSets(docs []config.Document) ([]ruleSet, error) {
	var sets []ruleSet
	for _, doc := range docs {
		if doc.Kind != "RuleSet" {
			continue
		}
		var payload struct {
			Spec ruleSetSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return nil, fmt.Errorf("render: parse rule set %s: %w", doc.Name, err)
		}
		value, err := payload.Spec.normalizedContent()
		if err != nil {
			return nil, fmt.Errorf("render: rule set %s: %w", doc.Name, err)
		}
		sets = append(sets, ruleSet{name: doc.Name, value: value})
	}
	return sets, nil
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

// ResolveNamedAsset resolves a single non-font Asset document by metadata.name.
// It returns the asset's serving value (URL path, remote URL, or data URI),
// its declared media type, and the local file that must be exposed through the
// HTTP server (nil for remote and inline sources). Local files are stat'ed
// during resolution, so a missing file surfaces here rather than at request time.
func ResolveNamedAsset(docs []config.Document, name string) (value, mediaType string, local *LocalAsset, err error) {
	for _, doc := range docs {
		if doc.Kind != "Asset" || doc.Name != name {
			continue
		}
		var payload struct {
			Spec assetSpec `json:"spec"`
		}
		if err := json.Unmarshal(doc.Raw, &payload); err != nil {
			return "", "", nil, fmt.Errorf("render: parse asset %s: %w", doc.Name, err)
		}
		value, local, err := resolveAssetValue(doc, payload.Spec, assetURLPath)
		if err != nil {
			return "", "", nil, err
		}
		return value, payload.Spec.MediaType, local, nil
	}
	return "", "", nil, fmt.Errorf("render: asset %q not found", name)
}

// fontURLPath generates a URL path for font assets.
func fontURLPath(name string) string {
	return "/assets/fonts/" + url.PathEscape(name)
}

// assetURLPath generates a URL path for file assets.
func assetURLPath(name string) string {
	return "/assets/files/" + url.PathEscape(name)
}
