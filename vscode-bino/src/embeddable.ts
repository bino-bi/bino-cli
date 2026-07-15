import type { KindInfo } from './indexer';

/**
 * Fallback render-embeddable component kinds, used only until the backend's
 * served `embeddable` flag has been fetched (see setRenderEmbeddableKinds).
 * Render-embeddable kinds render standalone as a component (the preview's live
 * canvas, the designer). The authoritative set is the Go `internal/report/embed`
 * package, served on GET /kinds and bino://kinds; the extension derives the live
 * set from that flag rather than maintaining this list. "Image" is a
 * layout-child kind, not a manifest kind, so it is intentionally absent; "Asset"
 * is a resource, not a standalone component.
 */
const FALLBACK_RENDER_EMBEDDABLE_KINDS = [
    'Table',
    'ChartStructure',
    'ChartTime',
    'ChartScatter',
    'ChartBubble',
    'Text',
    'Tree',
    'Grid'
];

/**
 * The current render-embeddable component kinds. Starts as the static fallback
 * and is replaced by the served `embeddable` flag once the indexer fetches it.
 */
let renderEmbeddableKinds: string[] = [...FALLBACK_RENDER_EMBEDDABLE_KINDS];

/**
 * Replace the render-embeddable set from the backend's served `embeddable` flag
 * (KindInfo.embeddable on GET /kinds). Called by the indexer after each index
 * refresh so the extension never drifts from the single Go authority. A served
 * list with no embeddable kinds is ignored so a partial/failed fetch can't blank
 * out the preview/designer entry points.
 */
export function setRenderEmbeddableKinds(kinds: KindInfo[]): void {
    const next = kinds.filter(k => k.embeddable).map(k => k.name);
    if (next.length > 0) {
        renderEmbeddableKinds = next;
    }
}

/**
 * Artefact / page kinds that are not standalone components but are still shown
 * in the embedded preview (rendered directly, or as a synthetic one-page
 * artefact). This is a separate UI concept from the render-embeddable flag above
 * and is intentionally an explicit, extension-owned list.
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
export function getEmbeddableKinds(): string[] {
    return [...ARTEFACT_PREVIEW_KINDS, ...renderEmbeddableKinds];
}

/** Returns true if a document of this kind can be shown in the embedded preview. */
export function isEmbeddableKind(kind: string): boolean {
    return getEmbeddableKinds().includes(kind);
}
