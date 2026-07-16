package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	reportspec "bino.bi/bino/internal/report/spec"
)

// dateString is a type alias for reportspec.DateString to simplify struct field declarations.
type dateString = reportspec.DateString

// layoutPageSpec defines the structure for LayoutPage components.
type layoutPageSpec struct {
	TitleBusinessUnit   string                   `json:"titleBusinessUnit"`
	TitleNamespace      string                   `json:"titleNamespace"`
	TitleDateStart      dateString               `json:"titleDateStart"`
	TitleDateEnd        dateString               `json:"titleDateEnd"`
	TitleDateFormat     string                   `json:"titleDateFormat"`
	TitleDateLink       string                   `json:"titleDateLink"`
	TitleMeasures       reportspec.MeasureList   `json:"titleMeasures"`
	TitleScenarios      reportspec.StringOrSlice `json:"titleScenarios"`
	TitleVariances      reportspec.StringOrSlice `json:"titleVariances"`
	TitleOrder          string                   `json:"titleOrder"`
	TitleOrderDirection string                   `json:"titleOrderDirection"`
	PageLayout          string                   `json:"pageLayout"`
	PageCustomTemplate  string                   `json:"pageCustomTemplate"`
	PageGridGap         string                   `json:"pageGridGap"`
	PageFormat          string                   `json:"pageFormat"`
	PageOrientation     string                   `json:"pageOrientation"`
	PageNumber          string                   `json:"pageNumber"`
	MessageText         string                   `json:"messageText"`
	MessageImage        string                   `json:"messageImage"`
	FooterText          string                   `json:"footerText"`
	PageFitToContent    *bool                    `json:"pageFitToContent"`
	FooterDisplayNumber *bool                    `json:"footerDisplayPageNumber"`
	SelectedStyle       string                   `json:"selectedStyle"`
	Ruleset             string                   `json:"ruleset"`
	Children            []layoutChild            `json:"children"`
}

func (s layoutPageSpec) writeAttrs(b *strings.Builder, assetURLs map[string]string) {
	writeAttr(b, "title-business-unit", renderInlineMarkdown(s.TitleBusinessUnit, assetURLs))
	writeAttr(b, "title-namespace", s.TitleNamespace)
	writeAttr(b, "title-date-start", s.TitleDateStart.String())
	writeAttr(b, "title-date-end", s.TitleDateEnd.String())
	writeAttr(b, "title-date-format", s.TitleDateFormat)
	writeAttr(b, "title-date-link", s.TitleDateLink)
	writeAttr(b, "title-measures", s.TitleMeasures.String())
	writeAttr(b, "title-scenarios", s.TitleScenarios.String())
	writeAttr(b, "title-variances", s.TitleVariances.String())
	writeAttr(b, "title-order", s.TitleOrder)
	writeAttr(b, "title-order-direction", s.TitleOrderDirection)
	writeAttr(b, "page-layout", s.PageLayout)
	writeAttr(b, "page-custom-template", s.PageCustomTemplate)
	writeAttr(b, "page-grid-gap", s.PageGridGap)
	writeAttr(b, "page-format", s.PageFormat)
	writeAttr(b, "page-orientation", s.PageOrientation)
	writeAttr(b, "page-number", s.PageNumber)
	writeAttr(b, "message-text", renderInlineMarkdown(s.MessageText, assetURLs))
	writeAttr(b, "message-image", s.MessageImage)
	writeAttr(b, "footer-text", s.FooterText)
	if s.PageFitToContent != nil {
		writeAttr(b, "page-fit-to-content", fmt.Sprintf("%t", *s.PageFitToContent))
	}
	if s.FooterDisplayNumber != nil {
		writeAttr(b, "footer-display-page-number", fmt.Sprintf("%t", *s.FooterDisplayNumber))
	}
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// layoutCardSpec defines the structure for LayoutCard components.
// Cards use card-* prefixed layout properties instead of page-* properties.
type layoutCardSpec struct {
	TitleImage          string                   `json:"titleImage"`
	TitleBusinessUnit   string                   `json:"titleBusinessUnit"`
	TitleScenarios      reportspec.StringOrSlice `json:"titleScenarios"`
	TitleVariances      reportspec.StringOrSlice `json:"titleVariances"`
	TitleOrder          string                   `json:"titleOrder"`
	TitleOrderDirection string                   `json:"titleOrderDirection"`
	TitleMeasures       reportspec.MeasureList   `json:"titleMeasures"`
	TitleDateStart      dateString               `json:"titleDateStart"`
	TitleDateEnd        dateString               `json:"titleDateEnd"`
	TitleDateFormat     string                   `json:"titleDateFormat"`
	TitleDateLink       string                   `json:"titleDateLink"`
	TitleNamespace      string                   `json:"titleNamespace"`
	FooterText          string                   `json:"footerText"`
	CardLayout          string                   `json:"cardLayout"`
	CardCustomTemplate  string                   `json:"cardCustomTemplate"`
	CardGridGap         string                   `json:"cardGridGap"`
	CardFitToContent    *bool                    `json:"cardFitToContent"`
	CardShowBorder      *bool                    `json:"cardShowBorder"`
	SelectedStyle       string                   `json:"selectedStyle"`
	Ruleset             string                   `json:"ruleset"`
	Children            []layoutChild            `json:"children"`
}

func (s layoutCardSpec) writeAttrs(b *strings.Builder, assetURLs map[string]string) {
	writeAttr(b, "title-image", s.TitleImage)
	writeAttr(b, "title-business-unit", renderInlineMarkdown(s.TitleBusinessUnit, assetURLs))
	writeAttr(b, "title-scenarios", s.TitleScenarios.String())
	writeAttr(b, "title-variances", s.TitleVariances.String())
	writeAttr(b, "title-order", s.TitleOrder)
	writeAttr(b, "title-order-direction", s.TitleOrderDirection)
	writeAttr(b, "title-measures", s.TitleMeasures.String())
	writeAttr(b, "title-date-start", s.TitleDateStart.String())
	writeAttr(b, "title-date-end", s.TitleDateEnd.String())
	writeAttr(b, "title-date-format", s.TitleDateFormat)
	writeAttr(b, "title-date-link", s.TitleDateLink)
	writeAttr(b, "title-namespace", s.TitleNamespace)
	writeAttr(b, "footer-text", s.FooterText)
	writeAttr(b, "card-layout", s.CardLayout)
	writeAttr(b, "card-custom-template", s.CardCustomTemplate)
	writeAttr(b, "card-grid-gap", s.CardGridGap)
	writeBoolAttr(b, "card-fit-to-content", s.CardFitToContent)
	writeBoolAttr(b, "card-show-border", s.CardShowBorder)
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// layoutChild represents a child component within a layout.
// It can be either an inline child (with spec) or a reference to a standalone document (with ref).
// When ref is set, the referenced document's spec is used as the base,
// and any spec fields provided here act as overrides.
// When optional is true and the ref is missing, the child is skipped gracefully instead of erroring.
type layoutChild struct {
	Kind     string            `json:"kind"`
	Metadata layoutChildMeta   `json:"metadata"`
	Ref      string            `json:"ref,omitempty"`
	Optional bool              `json:"optional,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Spec     json.RawMessage   `json:"spec,omitempty"`
}

// layoutChildMeta holds metadata for inline layout children.
type layoutChildMeta struct {
	Name        string `json:"name"`
	Constraints []any  `json:"constraints"` // Supports string or object format
}

// textSpec defines the structure for Text components.
type textSpec struct {
	Value         string                   `json:"value"`
	Dataset       reportspec.DatasetList   `json:"dataset"`
	Scale         reportspec.StringOrFloat `json:"scale,omitempty"`
	SelectedStyle string                   `json:"selectedStyle"`
}

// stackConfig defines the stacking configuration for chart components.
type stackConfig struct {
	By    string `json:"by"`
	Mode  string `json:"mode,omitempty"`
	Order string `json:"order,omitempty"`
}

// chartStructureSpec defines the structure for ChartStructure components.
type chartStructureSpec struct {
	Dataset                  reportspec.DatasetList   `json:"dataset"`
	ChartTitle               string                   `json:"chartTitle"`
	Filter                   string                   `json:"filter"`
	Level                    string                   `json:"level"`
	Order                    string                   `json:"order"`
	OrderDirection           string                   `json:"orderDirection"`
	MeasureScale             string                   `json:"measureScale"`
	MeasureUnit              string                   `json:"measureUnit"`
	PercentageScaling        reportspec.StringOrFloat `json:"percentageScaling"`
	UnitScaling              reportspec.StringOrFloat `json:"unitScaling"`
	Internationalisation     string                   `json:"internationalisation"`
	InternationalisationMode string                   `json:"internationalisationMode"`
	Translation              string                   `json:"translation"`
	ShowCategories           *bool                    `json:"showCategories"`
	ShowMeasureScale         *bool                    `json:"showMeasureScale"`
	Limit                    *int                     `json:"limit"`
	PixelPerUnit             *float64                 `json:"pixelPerUnit"`
	Scenarios                reportspec.StringOrSlice `json:"scenarios"`
	Variances                reportspec.StringOrSlice `json:"variances"`
	Stack                    *stackConfig             `json:"stack,omitempty"`
	Scale                    reportspec.StringOrFloat `json:"scale,omitempty"`
	SelectedStyle            string                   `json:"selectedStyle"`
	Ruleset                  string                   `json:"ruleset"`
}

func (s chartStructureSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "chart-title", s.ChartTitle)
	writeAttr(b, "filter", s.Filter)
	writeAttr(b, "level", s.Level)
	writeAttr(b, "order", s.Order)
	writeAttr(b, "order-direction", s.OrderDirection)
	writeAttr(b, "measure-scale", s.MeasureScale)
	writeAttr(b, "measure-unit", s.MeasureUnit)
	writeAttr(b, "percentage-scaling", s.PercentageScaling.String())
	writeAttr(b, "unit-scaling", s.UnitScaling.String())
	writeAttr(b, "internationalisation", s.Internationalisation)
	writeAttr(b, "internationalisation-mode", s.InternationalisationMode)
	writeAttr(b, "translation", s.Translation)
	writeBoolAttr(b, "show-categories", s.ShowCategories)
	writeBoolAttr(b, "show-measure-scale", s.ShowMeasureScale)
	writeIntAttr(b, "limit", s.Limit)
	writeFloatAttr(b, "pixel-per-unit", s.PixelPerUnit)
	writeAttr(b, "scenarios", s.Scenarios.String())
	writeAttr(b, "variances", s.Variances.String())
	writeStackAttr(b, "stack", s.Stack)
	writeAttr(b, "scale", s.Scale.String())
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// chartTimeSpec defines the structure for ChartTime components.
type chartTimeSpec struct {
	Dataset                  reportspec.DatasetList   `json:"dataset"`
	ChartTitle               string                   `json:"chartTitle"`
	ChartMode                string                   `json:"chartMode"`
	AxisLabelsMode           string                   `json:"axisLabelsMode"`
	DateInterval             string                   `json:"dateInterval"`
	Filter                   string                   `json:"filter"`
	Level                    string                   `json:"level"`
	Order                    string                   `json:"order"`
	OrderDirection           string                   `json:"orderDirection"`
	MeasureScale             string                   `json:"measureScale"`
	MeasureUnit              string                   `json:"measureUnit"`
	Internationalisation     string                   `json:"internationalisation"`
	InternationalisationMode string                   `json:"internationalisationMode"`
	Translation              string                   `json:"translation"`
	ShowCategories           *bool                    `json:"showCategories"`
	ShowMeasureScale         *bool                    `json:"showMeasureScale"`
	ShowOverlayAvg           *bool                    `json:"showOverlayAvg"`
	ShowOverlayMedian        *bool                    `json:"showOverlayMedian"`
	Limit                    *int                     `json:"limit"`
	MaxBars                  *int                     `json:"maxBars"`
	LineFullWidth            *bool                    `json:"lineFullWidth"`
	IntervalSpanLimit        *int                     `json:"intervalSpanLimit"`
	PercentageScaling        reportspec.StringOrFloat `json:"percentageScaling"`
	UnitScaling              reportspec.StringOrFloat `json:"unitScaling"`
	SyncSpaceLeft            *float64                 `json:"syncSpaceLeft"`
	Scenarios                reportspec.StringOrSlice `json:"scenarios"`
	Variances                reportspec.StringOrSlice `json:"variances"`
	Stack                    *stackConfig             `json:"stack,omitempty"`
	Scale                    reportspec.StringOrFloat `json:"scale,omitempty"`
	SelectedStyle            string                   `json:"selectedStyle"`
	Ruleset                  string                   `json:"ruleset"`
}

func (s chartTimeSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "chart-title", s.ChartTitle)
	writeAttr(b, "chart-mode", s.ChartMode)
	writeAttr(b, "axis-labels-mode", s.AxisLabelsMode)
	writeAttr(b, "date-interval", s.DateInterval)
	writeAttr(b, "filter", s.Filter)
	writeAttr(b, "level", s.Level)
	writeAttr(b, "order", s.Order)
	writeAttr(b, "order-direction", s.OrderDirection)
	writeAttr(b, "measure-scale", s.MeasureScale)
	writeAttr(b, "measure-unit", s.MeasureUnit)
	writeAttr(b, "internationalisation", s.Internationalisation)
	writeAttr(b, "internationalisation-mode", s.InternationalisationMode)
	writeAttr(b, "translation", s.Translation)
	writeBoolAttr(b, "show-categories", s.ShowCategories)
	writeBoolAttr(b, "show-measure-scale", s.ShowMeasureScale)
	writeBoolAttr(b, "show-overlay-avg", s.ShowOverlayAvg)
	writeBoolAttr(b, "show-overlay-median", s.ShowOverlayMedian)
	writeIntAttr(b, "limit", s.Limit)
	writeIntAttr(b, "max-bars", s.MaxBars)
	writeBoolAttr(b, "line-full-width", s.LineFullWidth)
	writeIntAttr(b, "interval-span-limit", s.IntervalSpanLimit)
	writeAttr(b, "percentage-scaling", s.PercentageScaling.String())
	writeAttr(b, "unit-scaling", s.UnitScaling.String())
	writeFloatAttr(b, "sync-space-left", s.SyncSpaceLeft)
	writeAttr(b, "scenarios", s.Scenarios.String())
	writeAttr(b, "variances", s.Variances.String())
	writeStackAttr(b, "stack", s.Stack)
	writeAttr(b, "scale", s.Scale.String())
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// chartScatterSpec defines the structure for ChartScatter components (bn-chart-scatter).
type chartScatterSpec struct {
	Dataset       reportspec.DatasetList   `json:"dataset"`
	ChartTitle    string                   `json:"chartTitle"`
	Filter        string                   `json:"filter"`
	X             json.RawMessage          `json:"x"`
	Y             json.RawMessage          `json:"y"`
	Iso           json.RawMessage          `json:"iso,omitempty"`
	Level         string                   `json:"level"`
	SeriesLevel   string                   `json:"seriesLevel"`
	Facet         json.RawMessage          `json:"facet,omitempty"`
	Labels        json.RawMessage          `json:"labels,omitempty"`
	Legend        json.RawMessage          `json:"legend,omitempty"`
	Aspect        string                   `json:"aspect"`
	Limit         *int                     `json:"limit"`
	Scale         reportspec.StringOrFloat `json:"scale,omitempty"`
	SelectedStyle string                   `json:"selectedStyle"`
	Ruleset       string                   `json:"ruleset"`
}

func (s chartScatterSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "chart-title", s.ChartTitle)
	writeAttr(b, "filter", s.Filter)
	writeMeasureAttr(b, "x", s.X)
	writeMeasureAttr(b, "y", s.Y)
	writeJSONObjAttr(b, "iso", s.Iso)
	writeAttr(b, "level", s.Level)
	writeAttr(b, "series-level", s.SeriesLevel)
	writeJSONObjAttr(b, "facet", s.Facet)
	writeJSONObjAttr(b, "labels", s.Labels)
	writeJSONObjAttr(b, "legend", s.Legend)
	writeAttr(b, "aspect", s.Aspect)
	writeIntAttr(b, "limit", s.Limit)
	writeAttr(b, "scale", s.Scale.String())
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// chartBubbleSpec defines the structure for ChartBubble components (bn-chart-bubble).
type chartBubbleSpec struct {
	Dataset       reportspec.DatasetList   `json:"dataset"`
	ChartTitle    string                   `json:"chartTitle"`
	Filter        string                   `json:"filter"`
	X             json.RawMessage          `json:"x"`
	Y             json.RawMessage          `json:"y"`
	Size          json.RawMessage          `json:"size"`
	Share         json.RawMessage          `json:"share,omitempty"`
	CompareWith   string                   `json:"compareWith"`
	Level         string                   `json:"level"`
	SeriesLevel   string                   `json:"seriesLevel"`
	Facet         json.RawMessage          `json:"facet,omitempty"`
	Labels        json.RawMessage          `json:"labels,omitempty"`
	Legend        json.RawMessage          `json:"legend,omitempty"`
	Aspect        string                   `json:"aspect"`
	Limit         *int                     `json:"limit"`
	Scale         reportspec.StringOrFloat `json:"scale,omitempty"`
	SelectedStyle string                   `json:"selectedStyle"`
	Ruleset       string                   `json:"ruleset"`
}

func (s chartBubbleSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "chart-title", s.ChartTitle)
	writeAttr(b, "filter", s.Filter)
	writeMeasureAttr(b, "x", s.X)
	writeMeasureAttr(b, "y", s.Y)
	writeMeasureAttr(b, "size", s.Size)
	writeMeasureAttr(b, "share", s.Share)
	writeAttr(b, "compare-with", s.CompareWith)
	writeAttr(b, "level", s.Level)
	writeAttr(b, "series-level", s.SeriesLevel)
	writeJSONObjAttr(b, "facet", s.Facet)
	writeJSONObjAttr(b, "labels", s.Labels)
	writeJSONObjAttr(b, "legend", s.Legend)
	writeAttr(b, "aspect", s.Aspect)
	writeIntAttr(b, "limit", s.Limit)
	writeAttr(b, "scale", s.Scale.String())
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// chartBulletSpec defines the structure for ChartBullet components (bn-chart-bullet).
type chartBulletSpec struct {
	Dataset        reportspec.DatasetList   `json:"dataset"`
	ChartTitle     string                   `json:"chartTitle"`
	Filter         string                   `json:"filter"`
	Actual         json.RawMessage          `json:"actual,omitempty"`
	Target         json.RawMessage          `json:"target,omitempty"`
	Ranges         []float64                `json:"ranges,omitempty"`
	Normalize      string                   `json:"normalize"`
	Variances      string                   `json:"variances"`
	Level          string                   `json:"level"`
	Order          string                   `json:"order"`
	OrderDirection string                   `json:"orderDirection"`
	Limit          *int                     `json:"limit"`
	Labels         json.RawMessage          `json:"labels,omitempty"`
	Scale          reportspec.StringOrFloat `json:"scale,omitempty"`
	SelectedStyle  string                   `json:"selectedStyle"`
	Ruleset        string                   `json:"ruleset"`
}

func (s chartBulletSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "chart-title", s.ChartTitle)
	writeAttr(b, "filter", s.Filter)
	writeMeasureAttr(b, "actual", s.Actual)
	writeMeasureAttr(b, "target", s.Target)
	writeFloatSliceAttr(b, "ranges", s.Ranges)
	writeAttr(b, "normalize", s.Normalize)
	writeAttr(b, "variances", s.Variances)
	writeAttr(b, "level", s.Level)
	writeAttr(b, "order", s.Order)
	writeAttr(b, "order-direction", s.OrderDirection)
	writeIntAttr(b, "limit", s.Limit)
	writeJSONObjAttr(b, "labels", s.Labels)
	writeAttr(b, "scale", s.Scale.String())
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// treeSpec defines the structure for Tree components.
// Trees display hierarchical structures with nodes connected by edges,
// commonly used for driver trees and decomposition diagrams.
type treeSpec struct {
	Edges         json.RawMessage `json:"edges"`
	Direction     string          `json:"direction"`
	LevelSpacing  *float64        `json:"levelSpacing"`
	NodeSpacing   *float64        `json:"nodeSpacing"`
	EdgeStyle     string          `json:"edgeStyle"`
	ShowOperators *bool           `json:"showOperators"`
	SelectedStyle string          `json:"selectedStyle"`
	Nodes         []treeNode      `json:"nodes"`
}

// treeNode defines a node in a tree.
// Each node can contain a Label, Table, ChartStructure, or ChartTime component.
// When optional is true and the ref is missing, the node is skipped gracefully instead of erroring.
type treeNode struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`
	Ref      string            `json:"ref,omitempty"`
	Optional bool              `json:"optional,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Spec     json.RawMessage   `json:"spec,omitempty"`
}

// treeLabelSpec defines a simple label component for tree nodes.
type treeLabelSpec struct {
	Value   string                   `json:"value"`
	Dataset reportspec.DatasetList   `json:"dataset"`
	Scale   reportspec.StringOrFloat `json:"scale,omitempty"`
}

func (s treeSpec) writeAttrs(b *strings.Builder) {
	// Write edges as JSON string attribute
	if len(s.Edges) > 0 {
		writeAttr(b, "edges", string(s.Edges))
	}
	writeAttr(b, "direction", s.Direction)
	writeFloatAttr(b, "level-spacing", s.LevelSpacing)
	writeFloatAttr(b, "node-spacing", s.NodeSpacing)
	writeAttr(b, "edge-style", s.EdgeStyle)
	writeBoolAttr(b, "show-operators", s.ShowOperators)
	writeAttr(b, "selected-style", s.SelectedStyle)
}

// tableSpec defines the structure for Table components.
type tableSpec struct {
	Dataset                  reportspec.DatasetList       `json:"dataset"`
	SumTitle                 string                       `json:"sumTitle"`
	Filter                   string                       `json:"filter"`
	Order                    string                       `json:"order"`
	OrderDirection           string                       `json:"orderDirection"`
	MeasureScale             string                       `json:"measureScale"`
	MeasureType              string                       `json:"measureType"`
	MeasureUnit              string                       `json:"measureUnit"`
	Internationalisation     string                       `json:"internationalisation"`
	InternationalisationMode string                       `json:"internationalisationMode"`
	Translation              string                       `json:"translation"`
	CategoryWidth            string                       `json:"categoryWidth"`
	DataFormat               string                       `json:"dataFormat"`
	DataFormatDigitsDecimal  *int                         `json:"dataFormatDigitsDecimal"`
	DataFormatDigitsPercent  *int                         `json:"dataFormatDigitsPercent"`
	Grouped                  *bool                        `json:"grouped"`
	ShowGroupTitle           *bool                        `json:"showGroupTitle"`
	ShowMeasureScale         *bool                        `json:"showMeasureScale"`
	Limit                    *int                         `json:"limit"`
	Type                     string                       `json:"type"`
	Scenarios                reportspec.StringOrSlice     `json:"scenarios"`
	Variances                reportspec.StringOrSlice     `json:"variances"`
	BarColumns               []string                     `json:"barColumns"`
	BarColumnWidth           string                       `json:"barColumnWidth"`
	UnitScaling              *float64                     `json:"unitScaling"`
	PercentageScaling        *float64                     `json:"percentageScaling"`
	Scale                    reportspec.StringOrFloat     `json:"scale,omitempty"`
	Thereof                  reportspec.ThereofList       `json:"thereof"`
	Partof                   reportspec.PartofList        `json:"partof"`
	Columnthereof            reportspec.ColumnthereofList `json:"columnthereof"`
	Interval                 string                       `json:"interval"`
	Attributes               reportspec.AttributesList    `json:"attributes"`
	SelectedStyle            string                       `json:"selectedStyle"`
	Ruleset                  string                       `json:"ruleset"`
}

func (s tableSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "sum-title", s.SumTitle)
	writeAttr(b, "filter", s.Filter)
	writeAttr(b, "order", s.Order)
	writeAttr(b, "order-direction", s.OrderDirection)
	writeAttr(b, "measure-scale", s.MeasureScale)
	writeAttr(b, "measure-type", s.MeasureType)
	writeAttr(b, "measure-unit", s.MeasureUnit)
	writeAttr(b, "internationalisation", s.Internationalisation)
	writeAttr(b, "internationalisation-mode", s.InternationalisationMode)
	writeAttr(b, "translation", s.Translation)
	writeAttr(b, "category-width", s.CategoryWidth)
	writeAttr(b, "data-format", s.DataFormat)
	writeIntAttr(b, "data-format-digits-decimal", s.DataFormatDigitsDecimal)
	writeIntAttr(b, "data-format-digits-percent", s.DataFormatDigitsPercent)
	writeBoolAttr(b, "grouped", s.Grouped)
	writeBoolAttr(b, "show-group-title", s.ShowGroupTitle)
	writeBoolAttr(b, "show-measure-scale", s.ShowMeasureScale)
	writeIntAttr(b, "limit", s.Limit)
	writeAttr(b, "type", s.Type)
	writeAttr(b, "scenarios", s.Scenarios.String())
	writeAttr(b, "variances", s.Variances.String())
	writeCSVAttr(b, "bar-columns", s.BarColumns)
	writeAttr(b, "bar-column-width", s.BarColumnWidth)
	writeFloatAttr(b, "unit-scaling", s.UnitScaling)
	writeFloatAttr(b, "percentage-scaling", s.PercentageScaling)
	writeAttr(b, "scale", s.Scale.String())
	writeAttr(b, "thereof", s.Thereof.String())
	writeAttr(b, "partof", s.Partof.String())
	writeAttr(b, "columnthereof", s.Columnthereof.String())
	writeAttr(b, "interval", s.Interval)
	writeAttr(b, "attributes", s.Attributes.String())
	writeAttr(b, "selected-style", s.SelectedStyle)
	writeAttr(b, "ruleset", s.Ruleset)
}

// writeBoolAttr writes a boolean attribute if the value is non-nil.
func writeBoolAttr(b *strings.Builder, name string, value *bool) {
	if value == nil {
		return
	}
	writeAttr(b, name, fmt.Sprintf("%t", *value))
}

// writeIntAttr writes an integer attribute if the value is non-nil.
func writeIntAttr(b *strings.Builder, name string, value *int) {
	if value == nil {
		return
	}
	writeAttr(b, name, strconv.Itoa(*value))
}

// writeFloatAttr writes a float attribute if the value is non-nil.
func writeFloatAttr(b *strings.Builder, name string, value *float64) {
	if value == nil {
		return
	}
	writeAttr(b, name, strconv.FormatFloat(*value, 'f', -1, 64))
}

// writeCSVAttr writes a comma-separated list attribute if non-empty.
func writeCSVAttr(b *strings.Builder, name string, values []string) {
	if len(values) == 0 {
		return
	}
	writeAttr(b, name, strings.Join(values, ","))
}

// writeFloatSliceAttr writes a numeric-array attribute as compact JSON if
// non-empty (e.g. ranges='[0.6,0.9]'). writeJSONObjAttr drops arrays.
func writeFloatSliceAttr(b *strings.Builder, name string, values []float64) {
	if len(values) == 0 {
		return
	}
	data, err := json.Marshal(values)
	if err != nil {
		return
	}
	writeAttr(b, name, string(data))
}

// writeStackAttr writes a stack config as a JSON string attribute.
func writeStackAttr(b *strings.Builder, name string, s *stackConfig) {
	if s == nil || s.By == "" {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	writeAttr(b, name, string(data))
}

// writeMeasureAttr writes a dual-form measure mapping attribute: a JSON string
// scalar is emitted as its bare token (x='ac1' — the engine treats any value
// not starting with '{' as shorthand for {"measure": value}), a JSON object is
// emitted compacted. Other JSON values are dropped; the schema rejects them.
func writeMeasureAttr(b *strings.Builder, name string, raw json.RawMessage) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return
	}
	var s string
	if err := json.Unmarshal(trimmed, &s); err == nil {
		writeAttr(b, name, s)
		return
	}
	writeJSONObjAttr(b, name, trimmed)
}

// writeJSONObjAttr writes an object-valued attribute as compact JSON.
// Non-object JSON values are dropped; the schema rejects them.
func writeJSONObjAttr(b *strings.Builder, name string, raw json.RawMessage) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return
	}
	writeAttr(b, name, buf.String())
}

// imageSpec defines the structure for Image components.
type imageSpec struct {
	Source        string `json:"source"`
	SelectedStyle string `json:"selectedStyle"`
}

func (s imageSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "source", s.Source)
	writeAttr(b, "selected-style", s.SelectedStyle)
}

// gridSpec defines the structure for Grid components.
// Grids create CSS grid-based layouts with row and column headers.
type gridSpec struct {
	ChartTitle        string          `json:"chartTitle"`
	RowHeaders        json.RawMessage `json:"rowHeaders"`
	ColumnHeaders     json.RawMessage `json:"columnHeaders"`
	ShowRowHeaders    *bool           `json:"showRowHeaders"`
	ShowColumnHeaders *bool           `json:"showColumnHeaders"`
	ShowBorders       *bool           `json:"showBorders"`
	RowHeaderWidth    string          `json:"rowHeaderWidth"`
	CellGap           string          `json:"cellGap"`
	SelectedStyle     string          `json:"selectedStyle"`
	Children          []gridChild     `json:"children"`
}

// gridChild defines a child (cell) in the grid.
type gridChild struct {
	Row      stringOrInt       `json:"row"`
	Column   stringOrInt       `json:"column"`
	Kind     string            `json:"kind"`
	Metadata layoutChildMeta   `json:"metadata"`
	Ref      string            `json:"ref,omitempty"`
	Optional bool              `json:"optional,omitempty"`
	Params   map[string]string `json:"params,omitempty"`
	Spec     json.RawMessage   `json:"spec,omitempty"`
}

// stringOrInt is a type that can unmarshal from either a string or an integer,
// converting integers to their string representation.
type stringOrInt string

func (s *stringOrInt) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = stringOrInt(str)
		return nil
	}
	// Try integer
	var num int
	if err := json.Unmarshal(data, &num); err == nil {
		*s = stringOrInt(strconv.Itoa(num))
		return nil
	}
	return fmt.Errorf("row/column must be string or integer, got: %s", string(data))
}

func (s stringOrInt) String() string {
	return string(s)
}

func (s gridSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "chart-title", s.ChartTitle)
	if len(s.RowHeaders) > 0 {
		writeAttr(b, "row-headers", string(s.RowHeaders))
	}
	if len(s.ColumnHeaders) > 0 {
		writeAttr(b, "column-headers", string(s.ColumnHeaders))
	}
	writeBoolAttr(b, "show-row-headers", s.ShowRowHeaders)
	writeBoolAttr(b, "show-column-headers", s.ShowColumnHeaders)
	writeBoolAttr(b, "show-borders", s.ShowBorders)
	writeAttr(b, "row-header-width", s.RowHeaderWidth)
	writeAttr(b, "cell-gap", s.CellGap)
	writeAttr(b, "selected-style", s.SelectedStyle)
}
