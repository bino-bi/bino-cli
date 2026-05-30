/**
 * Manifest kinds that the preview server can render standalone via
 * GET /__embedding/{name}?kind={kind}. Artefacts render directly; LayoutPages
 * and standalone components are rendered by synthesizing a one-page artefact.
 * LiveReportArtefact and data/config kinds are not embeddable.
 */
export const EMBEDDABLE_KINDS = [
    'ReportArtefact',
    'DocumentArtefact',
    'LayoutPage',
    'Table',
    'ChartStructure',
    'ChartTime',
    'Text',
    'Tree',
    'Grid',
    'Image'
];

/** Returns true if a document of this kind can be shown in the embedded preview. */
export function isEmbeddableKind(kind: string): boolean {
    return EMBEDDABLE_KINDS.includes(kind);
}
