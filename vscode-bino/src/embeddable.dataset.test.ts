import { describe, it, expect } from 'vitest';
import {
    isDesignerEditableKind,
    getDesignerEditableKinds,
    isEmbeddableKind,
    getEmbeddableKinds,
} from './embeddable';

// DataSet must be Designer-eligible (form-editable transforms) but NOT
// render-embeddable: it has no standalone canvas, so it must stay out of the
// embedded-preview / live-canvas / artefact-tree paths that assume a render.
describe('DataSet designer eligibility', () => {
    it('is designer-editable', () => {
        expect(isDesignerEditableKind('DataSet')).toBe(true);
        expect(getDesignerEditableKinds()).toContain('DataSet');
    });

    it('is NOT render-embeddable (no canvas)', () => {
        expect(isEmbeddableKind('DataSet')).toBe(false);
        expect(getEmbeddableKinds()).not.toContain('DataSet');
    });

    it('designer-editable set is a strict superset of the embeddable set', () => {
        const editable = getDesignerEditableKinds();
        for (const kind of getEmbeddableKinds()) {
            expect(editable).toContain(kind);
        }
        // The only addition is DataSet.
        expect(editable.length).toBe(getEmbeddableKinds().length + 1);
    });

    it('DataSource is neither designer-editable nor embeddable', () => {
        expect(isDesignerEditableKind('DataSource')).toBe(false);
        expect(isEmbeddableKind('DataSource')).toBe(false);
    });
});
