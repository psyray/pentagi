import { describe, expect, it } from 'vitest';

import { parseUnifiedDiff } from './skill-history-diff';

describe('parseUnifiedDiff', () => {
    it('returns nothing for an empty patch', () => {
        expect(parseUnifiedDiff('')).toEqual([]);
    });

    it('classifies headers, hunks, additions, removals and context', () => {
        const patch = ['--- v1', '+++ v2', '@@ -1,2 +1,3 @@', ' unchanged', '-old line', '+new line'].join('\n');

        expect(parseUnifiedDiff(patch)).toEqual([
            { kind: 'header', text: '--- v1' },
            { kind: 'header', text: '+++ v2' },
            { kind: 'hunk', text: '@@ -1,2 +1,3 @@' },
            { kind: 'context', text: ' unchanged' },
            { kind: 'remove', text: '-old line' },
            { kind: 'add', text: '+new line' },
        ]);
    });

    it('does not treat a file header as an added or removed line', () => {
        const kinds = parseUnifiedDiff('--- a\n+++ b').map((line) => line.kind);

        expect(kinds).toEqual(['header', 'header']);
    });

    it('drops the trailing newline instead of producing an empty last line', () => {
        expect(parseUnifiedDiff('+one\n')).toHaveLength(1);
        expect(parseUnifiedDiff('+one\n+two\n')).toHaveLength(2);
    });
});
