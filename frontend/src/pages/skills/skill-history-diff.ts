export interface DiffLine {
    kind: DiffLineKind;
    text: string;
}

export type DiffLineKind = 'add' | 'context' | 'header' | 'hunk' | 'remove';

/**
 * Splits a unified patch into typed lines. The backend sends one patch string;
 * classifying the lines here keeps the view free of prefix tests and lets the
 * classification be tested on its own.
 */
export function parseUnifiedDiff(patch: string): DiffLine[] {
    if (!patch) {
        return [];
    }

    const lines = patch.endsWith('\n') ? patch.slice(0, -1).split('\n') : patch.split('\n');

    return lines.map((text) => ({ kind: classify(text), text }));
}

function classify(line: string): DiffLineKind {
    if (line.startsWith('+++') || line.startsWith('---')) {
        return 'header';
    }

    if (line.startsWith('@@')) {
        return 'hunk';
    }

    if (line.startsWith('+')) {
        return 'add';
    }

    if (line.startsWith('-')) {
        return 'remove';
    }

    return 'context';
}
