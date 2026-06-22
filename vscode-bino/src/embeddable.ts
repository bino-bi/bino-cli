/**
 * Render-embeddable component kinds: manifest kinds that render standalone as a
 * component (the preview's live canvas, the designer). This mirrors the single
 * Go authority `internal/report/embed` (the served `embeddable` flag on
 * bino://kinds); keep the two in sync. "Image" is a layout-child kind, not a
 * manifest kind, so it is intentionally absent; "Asset" is a resource, not a
 * standalone component.
 */
export const RENDER_EMBEDDABLE_KINDS = [
    'Table',
    'ChartStructure',
    'ChartTime',
    'Text',
    'Tree',
    'Grid'
];

/**
 * Artefact / page kinds that are not standalone components but are still shown
 * in the embedded preview (rendered directly, or as a synthetic one-page
 * artefact). This is a separate UI concept from the render-component flag above.
 */
export const ARTEFACT_PREVIEW_KINDS = [
    'ReportArtefact',
    'DocumentArtefact',
    'LayoutPage'
];

/**
 * Manifest kinds the preview server can render standalone via
 * GET /__embedding/{name}?kind={kind}: the render-embeddable components plus the
 * artefact/page kinds. Used for preview/codelens visibility and the artefact
 * tree contextValue. LiveReportArtefact and data/config kinds are not
 * embeddable.
 */
export const EMBEDDABLE_KINDS = [
    ...ARTEFACT_PREVIEW_KINDS,
    ...RENDER_EMBEDDABLE_KINDS
];

/** Returns true if a document of this kind can be shown in the embedded preview. */
export function isEmbeddableKind(kind: string): boolean {
    return EMBEDDABLE_KINDS.includes(kind);
}
