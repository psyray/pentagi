import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';

// react-pdf bundles its own pdfjs-dist and re-exports it as `pdfjs`; the worker the
// preview points PDF.js at is imported from the project's own pdfjs-dist. Under pnpm
// the two only share a copy when the versions match. When they diverge, the worker
// throws "The API version ... does not match the Worker version ..." on its first
// message: react-pdf then suspends forever and the preview shows a spinner with no
// error. Pinning the project to the version react-pdf ships is what prevents that.
const readJson = (relative: string): Record<string, unknown> =>
    JSON.parse(readFileSync(path.join(process.cwd(), relative), 'utf8')) as Record<string, unknown>;

describe('report preview pdfjs wiring', () => {
    it('uses the same pdfjs-dist version as the react-pdf the export renders with', () => {
        const declared = (readJson('node_modules/react-pdf/package.json').dependencies as Record<string, string>)[
            'pdfjs-dist'
        ];
        const installed = readJson('node_modules/pdfjs-dist/package.json').version;

        // react-pdf pins an exact version; a range here would make the comparison below
        // meaningless (we would ship one version and load another's worker).
        expect(declared).toMatch(/^\d+\.\d+\.\d+$/);
        expect(installed).toBe(declared);
    });
});
