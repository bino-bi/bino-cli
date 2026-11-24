import { randomBytes } from 'crypto';

/** A fresh nonce for a webview's script-src CSP. */
export function getNonce(): string {
    return randomBytes(16).toString('hex');
}
