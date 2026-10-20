import { describe, expect, it } from 'vitest';

import { formatByteCount, formatBytes } from './report-format';

describe('formatBytes', () => {
    it('counts UTF-8 bytes, not UTF-16 code units', () => {
        // "é" is two bytes in UTF-8 but one code unit in a JS string.
        expect(formatBytes('é')).toBe('2 B');
    });

    it('rounds to whole kilobytes and switches to megabytes', () => {
        expect(formatBytes('a'.repeat(1024))).toBe('1 KB');
        expect(formatBytes('a'.repeat(1536))).toBe('2 KB');
        expect(formatBytes('a'.repeat(1024 * 1024))).toBe('1.0 MB');
    });

    it('handles the empty content', () => {
        expect(formatBytes('')).toBe('0 B');
    });
});

describe('formatByteCount', () => {
    it('renders a raw byte count coarsely, tolerating zero and junk', () => {
        expect(formatByteCount(0)).toBe('0 B');
        expect(formatByteCount(-5)).toBe('0 B');
        expect(formatByteCount(512)).toBe('512 B');
        expect(formatByteCount(2048)).toBe('2 KB');
        expect(formatByteCount(3 * 1024 * 1024)).toBe('3.0 MB');
    });
});
