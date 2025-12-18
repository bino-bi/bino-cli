package render

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	reportspec "bino.bi/bino/internal/report/spec"
)

// layoutPageSpec defines the structure for LayoutPage components.
type layoutPageSpec struct {
	TitleBusinessUnit   string                 `json:"titleBusinessUnit"`
	TitleNamespace      string                 `json:"titleNamespace"`
	TitleDateStart      string                 `json:"titleDateStart"`
	TitleDateEnd        string                 `json:"titleDateEnd"`
	TitleDateFormat     string                 `json:"titleDateFormat"`
	TitleDateLink       string                 `json:"titleDateLink"`
	TitleMeasures       reportspec.MeasureList `json:"titleMeasures"`
	TitleScenarios      string                 `json:"titleScenarios"`
	TitleVariances      string                 `json:"titleVariances"`
	TitleOrder          string                 `json:"titleOrder"`
	TitleOrderDirection string                 `json:"titleOrderDirection"`
	PageLayout          string                 `json:"pageLayout"`
	PageCustomTemplate  string                 `json:"pageCustomTemplate"`
	PageGridGap         string                 `json:"pageGridGap"`
	PageFormat          string                 `json:"pageFormat"`
	PageOrientation     string                 `json:"pageOrientation"`
	PageNumber          string                 `json:"pageNumber"`
	MessageText         string                 `json:"messageText"`
	MessageImage        string                 `json:"messageImage"`
	FooterText          string                 `json:"footerText"`
	PageFitToContent    *bool                  `json:"pageFitToContent"`
	FooterDisplayNumber *bool                  `json:"footerDisplayPageNumber"`
	Children            []layoutChild          `json:"children"`
}

func (s layoutPageSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "title-business-unit", s.TitleBusinessUnit)
	writeAttr(b, "title-namespace", s.TitleNamespace)
	writeAttr(b, "title-date-start", s.TitleDateStart)
	writeAttr(b, "title-date-end", s.TitleDateEnd)
	writeAttr(b, "title-date-format", s.TitleDateFormat)
	writeAttr(b, "title-date-link", s.TitleDateLink)
	writeAttr(b, "title-measures", s.TitleMeasures.String())
	writeAttr(b, "title-scenarios", s.TitleScenarios)
	writeAttr(b, "title-variances", s.TitleVariances)
	writeAttr(b, "title-order", s.TitleOrder)
	writeAttr(b, "title-order-direction", s.TitleOrderDirection)
	writeAttr(b, "page-layout", s.PageLayout)
	writeAttr(b, "page-custom-template", s.PageCustomTemplate)
	writeAttr(b, "page-grid-gap", s.PageGridGap)
	writeAttr(b, "page-format", s.PageFormat)
	writeAttr(b, "page-orientation", s.PageOrientation)
	writeAttr(b, "page-number", s.PageNumber)
	writeAttr(b, "message-text", s.MessageText)
	writeAttr(b, "message-image", s.MessageImage)
	writeAttr(b, "footer-text", s.FooterText)
	if s.PageFitToContent != nil {
		writeAttr(b, "page-fit-to-content", fmt.Sprintf("%t", *s.PageFitToContent))
	}
	if s.FooterDisplayNumber != nil {
		writeAttr(b, "footer-display-page-number", fmt.Sprintf("%t", *s.FooterDisplayNumber))
	}
}

// layoutCardSpec defines the structure for LayoutCard components.
// Cards use card-* prefixed layout properties instead of page-* properties.
type layoutCardSpec struct {
	TitleImage          string                 `json:"titleImage"`
	TitleBusinessUnit   string                 `json:"titleBusinessUnit"`
	TitleScenarios      string                 `json:"titleScenarios"`
	TitleVariances      string                 `json:"titleVariances"`
	TitleOrder          string                 `json:"titleOrder"`
	TitleOrderDirection string                 `json:"titleOrderDirection"`
	TitleMeasures       reportspec.MeasureList `json:"titleMeasures"`
	TitleDateStart      string                 `json:"titleDateStart"`
	TitleDateEnd        string                 `json:"titleDateEnd"`
	TitleDateFormat     string                 `json:"titleDateFormat"`
	TitleDateLink       string                 `json:"titleDateLink"`
	TitleNamespace      string                 `json:"titleNamespace"`
	FooterText          string                 `json:"footerText"`
	CardLayout          string                 `json:"cardLayout"`
	CardCustomTemplate  string                 `json:"cardCustomTemplate"`
	CardGridGap         string                 `json:"cardGridGap"`
	CardFitToContent    *bool                  `json:"cardFitToContent"`
	CardShowBorder      *bool                  `json:"cardShowBorder"`
	Children            []layoutChild          `json:"children"`
}

func (s layoutCardSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "title-image", s.TitleImage)
	writeAttr(b, "title-business-unit", s.TitleBusinessUnit)
	writeAttr(b, "title-scenarios", s.TitleScenarios)
	writeAttr(b, "title-variances", s.TitleVariances)
	writeAttr(b, "title-order", s.TitleOrder)
	writeAttr(b, "title-order-direction", s.TitleOrderDirection)
	writeAttr(b, "title-measures", s.TitleMeasures.String())
	writeAttr(b, "title-date-start", s.TitleDateStart)
	writeAttr(b, "title-date-end", s.TitleDateEnd)
	writeAttr(b, "title-date-format", s.TitleDateFormat)
	writeAttr(b, "title-date-link", s.TitleDateLink)
	writeAttr(b, "title-namespace", s.TitleNamespace)
	writeAttr(b, "footer-text", s.FooterText)
	writeAttr(b, "card-layout", s.CardLayout)
	writeAttr(b, "card-custom-template", s.CardCustomTemplate)
	writeAttr(b, "card-grid-gap", s.CardGridGap)
	writeBoolAttr(b, "card-fit-to-content", s.CardFitToContent)
	writeBoolAttr(b, "card-show-border", s.CardShowBorder)
}

// layoutChild represents a child component within a layout.
// It can be either an inline child (with spec) or a reference to a standalone document (with ref).
// When ref is set, the referenced document's spec is used as the base,
// and any spec fields provided here act as overrides.
type layoutChild struct {
	Kind     string          `json:"kind"`
	Metadata layoutChildMeta `json:"metadata"`
	Ref      string          `json:"ref,omitempty"`
	Spec     json.RawMessage `json:"spec,omitempty"`
}

// layoutChildMeta holds metadata for inline layout children.
type layoutChildMeta struct {
	Name        string   `json:"name"`
	Constraints []string `json:"constraints"`
}

// textSpec defines the structure for Text components.
type textSpec struct {
	Value   string                 `json:"value"`
	Dataset reportspec.DatasetList `json:"dataset"`
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
	ComponentStatus          string                   `json:"componentStatus"`
	Translation              string                   `json:"translation"`
	HideCategories           *bool                    `json:"hideCategories"`
	ShowCategories           *bool                    `json:"showCategories"`
	ShowMeasureScale         *bool                    `json:"showMeasureScale"`
	Limit                    *int                     `json:"limit"`
	PixelPerPercentage       *float64                 `json:"pixelPerPercentage"`
	PixelPerUnit             *float64                 `json:"pixelPerUnit"`
	Scenarios                []string                 `json:"scenarios"`
	Variances                []string                 `json:"variances"`
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
	writeAttr(b, "component-status", s.ComponentStatus)
	writeAttr(b, "translation", s.Translation)
	writeBoolAttr(b, "hide-categories", s.HideCategories)
	writeBoolAttr(b, "show-categories", s.ShowCategories)
	writeBoolAttr(b, "show-measure-scale", s.ShowMeasureScale)
	writeIntAttr(b, "limit", s.Limit)
	writeFloatAttr(b, "pixel-per-percentage", s.PixelPerPercentage)
	writeFloatAttr(b, "pixel-per-unit", s.PixelPerUnit)
	writeCSVAttr(b, "scenarios", s.Scenarios)
	writeCSVAttr(b, "variances", s.Variances)
}

// chartTimeSpec defines the structure for ChartTime components.
type chartTimeSpec struct {
	Dataset                         reportspec.DatasetList   `json:"dataset"`
	ChartTitle                      string                   `json:"chartTitle"`
	AxisLabelsMode                  string                   `json:"axisLabelsMode"`
	DateInterval                    string                   `json:"dateInterval"`
	Filter                          string                   `json:"filter"`
	Level                           string                   `json:"level"`
	Order                           string                   `json:"order"`
	OrderDirection                  string                   `json:"orderDirection"`
	MeasureScale                    string                   `json:"measureScale"`
	MeasureUnit                     string                   `json:"measureUnit"`
	Type                            string                   `json:"type"`
	Internationalisation            string                   `json:"internationalisation"`
	InternationalisationMode        string                   `json:"internationalisationMode"`
	ComponentStatus                 string                   `json:"componentStatus"`
	Translation                     string                   `json:"translation"`
	HideAxisLabels                  *bool                    `json:"hideAxisLabels"`
	ShowCategories                  *bool                    `json:"showCategories"`
	ShowMeasureScale                *bool                    `json:"showMeasureScale"`
	ShowOverlayAvg                  *bool                    `json:"showOverlayAvg"`
	ShowOverlayMedian               *bool                    `json:"showOverlayMedian"`
	Limit                           *int                     `json:"limit"`
	IntervalSpanLimit               *int                     `json:"intervalSpanLimit"`
	PercentageScaling               reportspec.StringOrFloat `json:"percentageScaling"`
	UnitScaling                     reportspec.StringOrFloat `json:"unitScaling"`
	SyncSpaceLeft                   *float64                 `json:"syncSpaceLeft"`
	PixelPerPercentage              *float64                 `json:"pixelPerPercentage"`
	PixelPerUnit                    *float64                 `json:"pixelPerUnit"`
	LeftSideOffsetForHorizontalSync *float64                 `json:"leftSideOffsetForHorizontalSync"`
	Scenarios                       []string                 `json:"scenarios"`
	Variances                       []string                 `json:"variances"`
}

func (s chartTimeSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "chart-title", s.ChartTitle)
	writeAttr(b, "axis-labels-mode", s.AxisLabelsMode)
	writeAttr(b, "date-interval", s.DateInterval)
	writeAttr(b, "filter", s.Filter)
	writeAttr(b, "level", s.Level)
	writeAttr(b, "order", s.Order)
	writeAttr(b, "order-direction", s.OrderDirection)
	writeAttr(b, "measure-scale", s.MeasureScale)
	writeAttr(b, "measure-unit", s.MeasureUnit)
	writeAttr(b, "type", s.Type)
	writeAttr(b, "internationalisation", s.Internationalisation)
	writeAttr(b, "internationalisation-mode", s.InternationalisationMode)
	writeAttr(b, "component-status", s.ComponentStatus)
	writeAttr(b, "translation", s.Translation)
	writeBoolAttr(b, "hide-axis-labels", s.HideAxisLabels)
	writeBoolAttr(b, "show-categories", s.ShowCategories)
	writeBoolAttr(b, "show-measure-scale", s.ShowMeasureScale)
	writeBoolAttr(b, "show-overlay-avg", s.ShowOverlayAvg)
	writeBoolAttr(b, "show-overlay-median", s.ShowOverlayMedian)
	writeIntAttr(b, "limit", s.Limit)
	writeIntAttr(b, "interval-span-limit", s.IntervalSpanLimit)
	writeAttr(b, "percentage-scaling", s.PercentageScaling.String())
	writeAttr(b, "unit-scaling", s.UnitScaling.String())
	writeFloatAttr(b, "sync-space-left", s.SyncSpaceLeft)
	writeFloatAttr(b, "pixel-per-percentage", s.PixelPerPercentage)
	writeFloatAttr(b, "pixel-per-unit", s.PixelPerUnit)
	writeFloatAttr(b, "left-side-offset-for-horizontal-sync", s.LeftSideOffsetForHorizontalSync)
	writeCSVAttr(b, "scenarios", s.Scenarios)
	writeCSVAttr(b, "variances", s.Variances)
}

// tableSpec defines the structure for Table components.
type tableSpec struct {
	Dataset                  reportspec.DatasetList       `json:"dataset"`
	TableTitle               string                       `json:"tableTitle"`
	Filter                   string                       `json:"filter"`
	Order                    string                       `json:"order"`
	OrderDirection           string                       `json:"orderDirection"`
	MeasureScale             string                       `json:"measureScale"`
	MeasureType              string                       `json:"measureType"`
	MeasureUnit              string                       `json:"measureUnit"`
	Internationalisation     string                       `json:"internationalisation"`
	InternationalisationMode string                       `json:"internationalisationMode"`
	ComponentStatus          string                       `json:"componentStatus"`
	Translation              string                       `json:"translation"`
	CategoryWidth            string                       `json:"categoryWidth"`
	DataFormat               string                       `json:"dataFormat"`
	DataFormatDigitsDecimal  *int                         `json:"dataFormatDigitsDecimal"`
	DataFormatDigitsPercent  *int                         `json:"dataFormatDigitsPercent"`
	Grouped                  *bool                        `json:"grouped"`
	ShowGroupTitle           *bool                        `json:"showGroupTitle"`
	ShowMeasureScale         *bool                        `json:"showMeasureScale"`
	Limit                    *int                         `json:"limit"`
	ScenariosDataFormat      string                       `json:"scenariosDataFormat"`
	Type                     string                       `json:"type"`
	Scenarios                []string                     `json:"scenarios"`
	Variances                []string                     `json:"variances"`
	Thereof                  reportspec.ThereofList       `json:"thereof"`
	Partof                   reportspec.PartofList        `json:"partof"`
	Columnthereof            reportspec.ColumnthereofList `json:"columnthereof"`
}

func (s tableSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "datasets", s.Dataset.Join(","))
	writeAttr(b, "table-title", s.TableTitle)
	writeAttr(b, "filter", s.Filter)
	writeAttr(b, "order", s.Order)
	writeAttr(b, "order-direction", s.OrderDirection)
	writeAttr(b, "measure-scale", s.MeasureScale)
	writeAttr(b, "measure-type", s.MeasureType)
	writeAttr(b, "measure-unit", s.MeasureUnit)
	writeAttr(b, "internationalisation", s.Internationalisation)
	writeAttr(b, "internationalisation-mode", s.InternationalisationMode)
	writeAttr(b, "component-status", s.ComponentStatus)
	writeAttr(b, "translation", s.Translation)
	writeAttr(b, "category-width", s.CategoryWidth)
	writeAttr(b, "data-format", s.DataFormat)
	writeIntAttr(b, "data-format-digits-decimal", s.DataFormatDigitsDecimal)
	writeIntAttr(b, "data-format-digits-percent", s.DataFormatDigitsPercent)
	writeBoolAttr(b, "grouped", s.Grouped)
	writeBoolAttr(b, "show-group-title", s.ShowGroupTitle)
	writeBoolAttr(b, "show-measure-scale", s.ShowMeasureScale)
	writeIntAttr(b, "limit", s.Limit)
	writeAttr(b, "scenarios-data-format", s.ScenariosDataFormat)
	writeAttr(b, "type", s.Type)
	writeCSVAttr(b, "scenarios", s.Scenarios)
	writeCSVAttr(b, "variances", s.Variances)
	writeAttr(b, "thereof", s.Thereof.String())
	writeAttr(b, "partof", s.Partof.String())
	writeAttr(b, "columnthereof", s.Columnthereof.String())
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

// imageSpec defines the structure for Image components.
type imageSpec struct {
	Source string `json:"source"`
}

func (s imageSpec) writeAttrs(b *strings.Builder) {
	writeAttr(b, "source", s.Source)
}
