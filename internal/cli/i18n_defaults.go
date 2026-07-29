package cli

import "sort"

// defaultI18nTokens is a snapshot of the template engine's built-in
// translation bundles (the "_system" namespace), flattened to the dotted key
// form the engine stores them in.
//
// Source of truth: bn-template-engine/src/stores/internationalization.ts —
// keep this file in sync when the engine's default bundles change.
var defaultI18nTokens = map[string]map[string]string{
	"de": {
		"global.ac1":                          "AC",
		"global.pp1":                          "PY",
		"global.fc1":                          "FC",
		"global.pl1":                          "PL",
		"global.ac2":                          "AC2",
		"global.pp2":                          "PY2",
		"global.fc2":                          "FC2",
		"global.pl2":                          "PL2",
		"global.ac3":                          "AC3",
		"global.pp3":                          "PY3",
		"global.fc3":                          "FC3",
		"global.pl3":                          "PL3",
		"global.ac4":                          "AC4",
		"global.pp4":                          "PY4",
		"global.fc4":                          "FC4",
		"global.pl4":                          "PL4",
		"global.ORDER_asc":                    "↑",
		"global.ORDER_desc":                   "↓",
		"global.ibcssymbol_delta_ac":          "Δ{%}",
		"global.ibcssymbol_delta_generic":     "Δ({%})",
		"global.ibcssymbol_delta_ac_relative": "Δ{%}%",
		"global.ibcssymbol_delta_generic_relative": "Δ({%})%",

		"bn-title.SEPERATOR_WS": ", ", //nolint:misspell // engine key is spelled this way
		"bn-title.CONNECTOR_WS": " und ",
		"bn-title.in":           "in",

		"bn-table.in":        "in",
		"bn-table.category":  " ",
		"bn-table.operation": " ",
		"bn-table.there_of":  "davon",
		"bn-table.part_of":   "in % von",
		"bn-table.SUM_TOTAL": "❖",
		"bn-table.REST":      "REST",
		"bn-table.no-data":   "==Keine Daten==",

		"bn-chart-time.no-data": "==Keine Daten==",

		"bn-chart-structure.no-data": "==Keine Daten==",
		"bn-chart-structure.REST":    "REST",

		"bn-chart-scatter.axis.in":            "in",
		"bn-chart-scatter.legend.title":       "",
		"bn-chart-scatter.no-data":            "==Keine Daten==",
		"bn-chart-scatter.xy-prop-json":       "{{prop}}: ungültiges JSON.",
		"bn-chart-scatter.xy-measure-invalid": "{{prop}}: '{{measure}}' ist weder eine Szenario-Spalte noch ein Varianz-Token.",
		"bn-chart-scatter.xy-family-mixed":    "Alle Kennzahlen müssen zur selben Szenario-Familie gehören (gefunden: {{families}}).",
		"bn-chart-scatter.xy-level-order":     "Facet-, Serien- und Punktebene müssen in der Hierarchie streng absteigen ({{detail}}).",
		"bn-chart-scatter.xy-duplicate-point": "{{count}} doppelte Punkt-Identität(en) verworfen (erste Zeile gewinnt): {{names}}. Im DataSet-SQL aggregieren.",
		"bn-chart-scatter.xy-point-clipped":   "{{count}} Punkt(e) außerhalb des expliziten min/max ausgeschlossen.",
		"bn-chart-scatter.xy-variance-nodata": "{{count}} Zeile(n) ohne Varianzdaten ausgeschlossen.",
		"bn-chart-scatter.xy-label-unknown":   "Unbekannte Label-Namen: {{names}}.",
		"bn-chart-scatter.xy-series-overflow": "Mehr Serien als Palettenfarben; Farben wiederholen sich aufgehellt.",
		"bn-chart-scatter.xy-point-budget":    "{{count}} Punkte überschreiten das Budget von 5000 Punkten; 'limit' erwägen.",
		"bn-chart-scatter.xy-iso-domain":      "Iso-Linien benötigen einen strikt positiven Wertebereich auf beiden Achsen; übersprungen.",

		"bn-chart-bubble.axis.in":                    "in",
		"bn-chart-bubble.legend.title":               "",
		"bn-chart-bubble.legend.size":                "Größe",
		"bn-chart-bubble.legend.compare":             "Szenarien",
		"bn-chart-bubble.no-data":                    "==Keine Daten==",
		"bn-chart-bubble.xy-prop-json":               "{{prop}}: ungültiges JSON.",
		"bn-chart-bubble.xy-measure-invalid":         "{{prop}}: '{{measure}}' ist weder eine Szenario-Spalte noch ein Varianz-Token.",
		"bn-chart-bubble.xy-family-mixed":            "Alle Kennzahlen müssen zur selben Szenario-Familie gehören (gefunden: {{families}}).",
		"bn-chart-bubble.xy-level-order":             "Facet-, Serien- und Punktebene müssen in der Hierarchie streng absteigen ({{detail}}).",
		"bn-chart-bubble.xy-duplicate-point":         "{{count}} doppelte Punkt-Identität(en) verworfen (erste Zeile gewinnt): {{names}}. Im DataSet-SQL aggregieren.",
		"bn-chart-bubble.xy-point-clipped":           "{{count}} Punkt(e) außerhalb des expliziten min/max ausgeschlossen.",
		"bn-chart-bubble.xy-variance-nodata":         "{{count}} Zeile(n) ohne Varianzdaten ausgeschlossen.",
		"bn-chart-bubble.xy-label-unknown":           "Unbekannte Label-Namen: {{names}}.",
		"bn-chart-bubble.xy-series-overflow":         "Mehr Serien als Palettenfarben; Farben wiederholen sich aufgehellt.",
		"bn-chart-bubble.xy-point-budget":            "{{count}} Punkte überschreiten das Budget von 5000 Punkten; 'limit' erwägen.",
		"bn-chart-bubble.xy-compare-variance":        "compareWith kann nicht mit Varianz-Kennzahlen auf den Achsen kombiniert werden.",
		"bn-chart-bubble.bubble-size-negative":       "{{count}} Blase(n) mit negativem Größenwert ausgeschlossen.",
		"bn-chart-bubble.bubble-share-range":         "{{count}} Anteilswert(e) außerhalb [0,1] wurden begrenzt.",
		"bn-chart-bubble.bubble-scale-group-unknown": "size.group '{{group}}' verweist auf keine bn-scaling-group im Kontext.",
		"bn-chart-bubble.bubble-scale-overflow":      "{{count}} gruppenskalierte Blase(n) überschreiten das Panel; in Originalgröße gezeichnet.",

		"bn-chart-bullet.axis.in":                "in",
		"bn-chart-bullet.no-data":                "==Keine Daten==",
		"bn-chart-bullet.target":                 "Ziel",
		"bn-chart-bullet.bullet-prop-json":       "{{prop}}: ungültiges JSON.",
		"bn-chart-bullet.bullet-measure-invalid": "{{prop}}: '{{measure}}' ist keine Szenario-Spalte (ac1…pl4).",
		"bn-chart-bullet.bullet-measure-same":    "actual und target müssen unterschiedliche Szenarien sein ('{{measure}}').",
		"bn-chart-bullet.bullet-target-none":     "Kein Ziel-Szenario gefunden (pl1/pp1/fc1); nur Ist-Balken werden gezeichnet.",
		"bn-chart-bullet.bullet-target-missing":  "{{count}} Zeile(n) ohne Zielwert; Varianz und Marker entfallen.",
		"bn-chart-bullet.bullet-target-invalid":  "{{count}} Zeile(n) mit Ziel ≤ 0 können nicht normalisiert werden; Balken entfällt.",
		"bn-chart-bullet.bullet-mixed-operation": "{{count}} KPI-Gruppe(n) mischen '+'- und '-'-Zeilen; Sentiment fällt auf '+' zurück.",
		"bn-chart-bullet.bullet-ranges-ignored":  `ranges: im normalisierten (IBCS-)Modus ignoriert; normalize="none" setzen.`,
		"bn-chart-bullet.bullet-ranges-invalid":  "ranges: erwartet bis zu 2 aufsteigende positive Zielanteile ({{detail}}).",
	},
	"en": {
		"global.ac1":                          "AC",
		"global.pp1":                          "PY",
		"global.fc1":                          "FC",
		"global.pl1":                          "PL",
		"global.ac2":                          "AC2",
		"global.pp2":                          "PY2",
		"global.fc2":                          "FC2",
		"global.pl2":                          "PL2",
		"global.ac3":                          "AC3",
		"global.pp3":                          "PY3",
		"global.fc3":                          "FC3",
		"global.pl3":                          "PL3",
		"global.ac4":                          "AC4",
		"global.pp4":                          "PY4",
		"global.fc4":                          "FC4",
		"global.pl4":                          "PL4",
		"global.ORDER_asc":                    "↑",
		"global.ORDER_desc":                   "↓",
		"global.ibcssymbol_delta_ac":          "Δ{%}",
		"global.ibcssymbol_delta_generic":     "Δ({%})",
		"global.ibcssymbol_delta_ac_relative": "Δ{%}%",
		"global.ibcssymbol_delta_generic_relative": "Δ({%})%",

		"bn-title.SEPERATOR_WS": ", ", //nolint:misspell // engine key is spelled this way
		"bn-title.CONNECTOR_WS": " and ",
		"bn-title.in":           "in",

		"bn-table.in":        "in",
		"bn-table.category":  " ",
		"bn-table.operation": " ",
		"bn-table.there_of":  "there of",
		"bn-table.part_of":   "in % of",
		"bn-table.SUM_TOTAL": "❖",
		"bn-table.REST":      "REST",
		"bn-table.no-data":   "==No Data==",

		"bn-chart-time.no-data": "==No Data==",

		"bn-chart-structure.no-data": "==No Data==",
		"bn-chart-structure.REST":    "REST",

		"bn-chart-scatter.axis.in":            "in",
		"bn-chart-scatter.legend.title":       "",
		"bn-chart-scatter.no-data":            "==No Data==",
		"bn-chart-scatter.xy-prop-json":       "{{prop}}: invalid JSON.",
		"bn-chart-scatter.xy-measure-invalid": "{{prop}}: '{{measure}}' is neither a scenario column nor a variance token.",
		"bn-chart-scatter.xy-family-mixed":    "All measures must share one scenario family (found: {{families}}).",
		"bn-chart-scatter.xy-level-order":     "Facet, series, and point levels must strictly descend in the hierarchy ({{detail}}).",
		"bn-chart-scatter.xy-duplicate-point": "{{count}} duplicate point identitie(s) dropped (first row wins): {{names}}. Aggregate in the DataSet SQL.",
		"bn-chart-scatter.xy-point-clipped":   "{{count}} point(s) outside the explicit min/max excluded.",
		"bn-chart-scatter.xy-variance-nodata": "{{count}} row(s) without variance data excluded.",
		"bn-chart-scatter.xy-label-unknown":   "Unknown label name(s): {{names}}.",
		"bn-chart-scatter.xy-series-overflow": "More series than palette colors; colors repeat with a tint step.",
		"bn-chart-scatter.xy-point-budget":    "{{count}} points exceed the 5000-point budget; consider 'limit'.",
		"bn-chart-scatter.xy-iso-domain":      "Iso-lines need a strictly positive domain on both axes; skipped.",

		"bn-chart-bubble.axis.in":                    "in",
		"bn-chart-bubble.legend.title":               "",
		"bn-chart-bubble.legend.size":                "Size",
		"bn-chart-bubble.legend.compare":             "Scenarios",
		"bn-chart-bubble.no-data":                    "==No Data==",
		"bn-chart-bubble.xy-prop-json":               "{{prop}}: invalid JSON.",
		"bn-chart-bubble.xy-measure-invalid":         "{{prop}}: '{{measure}}' is neither a scenario column nor a variance token.",
		"bn-chart-bubble.xy-family-mixed":            "All measures must share one scenario family (found: {{families}}).",
		"bn-chart-bubble.xy-level-order":             "Facet, series, and point levels must strictly descend in the hierarchy ({{detail}}).",
		"bn-chart-bubble.xy-duplicate-point":         "{{count}} duplicate point identitie(s) dropped (first row wins): {{names}}. Aggregate in the DataSet SQL.",
		"bn-chart-bubble.xy-point-clipped":           "{{count}} point(s) outside the explicit min/max excluded.",
		"bn-chart-bubble.xy-variance-nodata":         "{{count}} row(s) without variance data excluded.",
		"bn-chart-bubble.xy-label-unknown":           "Unknown label name(s): {{names}}.",
		"bn-chart-bubble.xy-series-overflow":         "More series than palette colors; colors repeat with a tint step.",
		"bn-chart-bubble.xy-point-budget":            "{{count}} points exceed the 5000-point budget; consider 'limit'.",
		"bn-chart-bubble.xy-compare-variance":        "compareWith cannot be combined with variance measures on the axes.",
		"bn-chart-bubble.bubble-size-negative":       "{{count}} bubble(s) with a negative size value excluded.",
		"bn-chart-bubble.bubble-share-range":         "{{count}} share value(s) outside [0,1] were clamped.",
		"bn-chart-bubble.bubble-scale-group-unknown": "size.group '{{group}}' names no bn-scaling-group in scope.",
		"bn-chart-bubble.bubble-scale-overflow":      "{{count}} group-scaled bubble(s) exceed the panel; rendered true-size.",

		"bn-chart-bullet.axis.in":                "in",
		"bn-chart-bullet.no-data":                "==No Data==",
		"bn-chart-bullet.target":                 "Target",
		"bn-chart-bullet.bullet-prop-json":       "{{prop}}: invalid JSON.",
		"bn-chart-bullet.bullet-measure-invalid": "{{prop}}: '{{measure}}' is not a scenario column (ac1…pl4).",
		"bn-chart-bullet.bullet-measure-same":    "actual and target must map different scenarios ('{{measure}}').",
		"bn-chart-bullet.bullet-target-none":     "No target scenario found (pl1/pp1/fc1); rendering actual bars only.",
		"bn-chart-bullet.bullet-target-missing":  "{{count}} row(s) without a target value; variance and marker omitted.",
		"bn-chart-bullet.bullet-target-invalid":  "{{count}} row(s) with target ≤ 0 cannot be normalized; bar omitted.",
		"bn-chart-bullet.bullet-mixed-operation": "{{count}} KPI group(s) mix '+' and '-' rows; sentiment defaults to '+'.",
		"bn-chart-bullet.bullet-ranges-ignored":  `ranges: ignored in normalized (IBCS) mode; set normalize="none".`,
		"bn-chart-bullet.bullet-ranges-invalid":  "ranges: expected up to 2 ascending positive fractions of the target ({{detail}}).",
	},
}

// defaultI18nLocales returns the locale codes covered by defaultI18nTokens in
// sorted order.
func defaultI18nLocales() []string {
	locales := make([]string, 0, len(defaultI18nTokens))
	for code := range defaultI18nTokens {
		locales = append(locales, code)
	}
	sort.Strings(locales)
	return locales
}
