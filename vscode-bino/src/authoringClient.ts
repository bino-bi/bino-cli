import * as vscode from 'vscode';
import * as path from 'path';
import { WorkspaceIndexer, LSPEditDiagnostic, LSPEditResult } from './indexer';

/**
 * How an applied edit reached disk/buffer.
 * - `workspaceEdit`: the target manifest was open in a TextDocument, so the edit
 *   merged through a `vscode.WorkspaceEdit` (one undo step, dirty-state honored,
 *   the LSP re-diagnoses from the resulting didChange).
 * - `write`: the manifest was closed, so it was written atomically and the
 *   watcher re-indexes it.
 */
export type EditApplied = 'workspaceEdit' | 'write';

/** Outcome of an authoring mutation. */
export type EditResult =
    | { ok: true; applied: EditApplied }
    | { ok: false; diagnostics: LSPEditDiagnostic[]; error?: string };

/** A dotted-path edit request (set existing or auto-vivify nested maps). */
export interface EditRequest {
    /** Absolute path to the manifest file. */
    file: string;
    /** 1-based document ordinal within a multi-document file (default 1). */
    position?: number;
    /** Dotted-path patch map, e.g. { "spec.title": "Q3" }. */
    patch: Record<string, unknown>;
}

/** A removal request (one or more dotted paths within a single document). */
export interface RemoveRequest {
    file: string;
    position?: number;
    /** Dotted paths to delete, e.g. ["spec.title", "spec.columns[1]"]. */
    paths: string[];
}

/**
 * An append request: grow the sequence at `path` by one element. A missing
 * sequence (and its intermediate maps) is created, so appending to an absent
 * array yields a one-element sequence. This is the only mutation that grows a
 * sequence past its end — an operation the set-only edit op cannot express.
 */
export interface AppendRequest {
    file: string;
    position?: number;
    /** Dotted path of the sequence to append to, e.g. "spec.children". */
    path: string;
    /** The element to append (scalar, object, or array). */
    value: unknown;
}

/**
 * The single extension-side authoring client every Design surface mutates
 * manifests through. It computes the rewritten manifest via the one Go fidelity
 * engine (`bino lsp-helper edit`, which preserves comments and key order) and
 * applies it coherently with open buffers and the LSP:
 *
 * - file OPEN in a TextDocument -> whole-range `vscode.WorkspaceEdit` with the
 *   engine's `full` text (one undo step, no "file changed on disk" prompt);
 * - file CLOSED -> atomic write via the helper, picked up by the watcher.
 *
 * Both paths refuse an edit the engine rejects (schema diagnostics) and surface
 * the diagnostic instead of writing.
 */
export class AuthoringClient {
    constructor(private readonly indexer: WorkspaceIndexer) {}

    /** Apply a dotted-path patch to a manifest document. */
    async edit(req: EditRequest): Promise<EditResult> {
        return this.apply(req.file, req.position, { op: 'edit', patch: req.patch });
    }

    /** Remove one or more dotted paths from a manifest document. */
    async remove(req: RemoveRequest): Promise<EditResult> {
        return this.apply(req.file, req.position, { op: 'remove', paths: req.paths });
    }

    /** Append an element to the sequence at a dotted path (creating it if absent). */
    async append(req: AppendRequest): Promise<EditResult> {
        return this.apply(req.file, req.position, { op: 'append', path: req.path, value: req.value });
    }

    /**
     * Compute the rewritten manifest for a mutation and apply it through the
     * open buffer (WorkspaceEdit) or an atomic write, gated on diagnostics.
     */
    private async apply(
        file: string,
        position: number | undefined,
        opPayload: Record<string, unknown>
    ): Promise<EditResult> {
        const abs = path.isAbsolute(file) ? file : path.resolve(file);
        const open = vscode.workspace.textDocuments.find(d => d.uri.fsPath === abs);
        const base = { file: abs, position: position ?? 1, ...opPayload };

        if (open) {
            // Open buffer: compute the rewritten file, then merge via WorkspaceEdit
            // so undo is one step and the editor's dirty-state is honored.
            const result = await this.computeOrFail({ ...base, mode: 'compute' });
            if (!result.ok) { return result; }
            const full = result.value.full;
            if (full === undefined) {
                return { ok: false, diagnostics: [], error: 'compute returned no content' };
            }
            const fullRange = new vscode.Range(
                open.positionAt(0),
                open.positionAt(open.getText().length)
            );
            const edit = new vscode.WorkspaceEdit();
            edit.replace(open.uri, fullRange, full);
            const applied = await vscode.workspace.applyEdit(edit);
            if (!applied) {
                return { ok: false, diagnostics: [], error: 'workspace edit was not applied' };
            }
            return { ok: true, applied: 'workspaceEdit' };
        }

        // Closed file: the helper validates and writes atomically only if valid;
        // the watcher re-indexes within ~0.6s with no manual ping.
        const result = await this.computeOrFail({ ...base, mode: 'write' });
        if (!result.ok) { return result; }
        return { ok: true, applied: 'write' };
    }

    /**
     * Run the edit helper and split its result into a failure (error or
     * diagnostics — nothing was written) or the raw success payload.
     */
    private async computeOrFail(
        payload: Record<string, unknown>
    ): Promise<{ ok: true; value: LSPEditResult } | { ok: false; diagnostics: LSPEditDiagnostic[]; error?: string }> {
        let result: LSPEditResult;
        try {
            result = await this.indexer.editManifest(payload);
        } catch (err) {
            return { ok: false, diagnostics: [], error: err instanceof Error ? err.message : String(err) };
        }
        if (result.error) {
            return { ok: false, diagnostics: result.diagnostics ?? [], error: result.error };
        }
        if (result.diagnostics && result.diagnostics.length > 0) {
            return { ok: false, diagnostics: result.diagnostics };
        }
        if (!result.ok) {
            return { ok: false, diagnostics: [], error: 'edit failed' };
        }
        return { ok: true, value: result };
    }
}

/** Render edit diagnostics into a single human-readable message for the GUI. */
export function formatEditDiagnostics(result: { diagnostics: LSPEditDiagnostic[]; error?: string }): string {
    if (result.diagnostics.length > 0) {
        return result.diagnostics.map(d => d.message).join('; ');
    }
    return result.error ?? 'The edit was rejected.';
}
