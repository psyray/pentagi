import { describe, expect, it } from 'vitest';

import {
    arrayBufferToDataUrl,
    brandingHasPageTokens,
    resolvePageTokens,
    scaleImage,
    splitPageTokens,
} from './report-branding';

describe('splitPageTokens', () => {
    it('keeps the page placeholders apart from the text', () => {
        expect(splitPageTokens('Page [[page]]/[[pages]]')).toEqual([
            { kind: 'text', value: 'Page ' },
            { kind: 'page', value: '' },
            { kind: 'text', value: '/' },
            { kind: 'pages', value: '' },
        ]);
    });

    it('returns the text untouched when there is no placeholder', () => {
        expect(splitPageTokens('Acme')).toEqual([{ kind: 'text', value: 'Acme' }]);
        expect(splitPageTokens('')).toEqual([]);
    });
});

describe('brandingHasPageTokens / resolvePageTokens', () => {
    it('detects and substitutes the renderer-owned tokens', () => {
        expect(brandingHasPageTokens('Acme · [[date]]')).toBe(false);
        expect(brandingHasPageTokens('Acme · [[page]]')).toBe(true);

        expect(resolvePageTokens('Page [[page]] of [[pages]]', 3, 12)).toBe('Page 3 of 12');
        expect(resolvePageTokens('Acme', 1, 2)).toBe('Acme');
    });
});

describe('scaleImage', () => {
    it('keeps the aspect inside the maximum width', () => {
        expect(scaleImage({ height: 100, width: 200 }, 100)).toEqual({ height: 50, width: 100 });
    });

    it('leaves a small image alone', () => {
        expect(scaleImage({ height: 20, width: 30 }, 100)).toEqual({ height: 20, width: 30 });
    });

    it('never divides by an empty width', () => {
        expect(scaleImage({ height: 0, width: 0 }, 100)).toEqual({ height: 0, width: 0 });
    });
});

describe('arrayBufferToDataUrl', () => {
    it('builds a data URL from the raw bytes', () => {
        const bytes = new Uint8Array([0x89, 0x50, 0x4e, 0x47]);

        expect(arrayBufferToDataUrl(bytes.buffer, 'image/png')).toBe('data:image/png;base64,iVBORw==');
    });

    it('handles a payload larger than the chunk size', () => {
        const bytes = new Uint8Array(0x8000 + 10);
        const dataUrl = arrayBufferToDataUrl(bytes.buffer, 'image/png');

        expect(dataUrl.startsWith('data:image/png;base64,')).toBe(true);
        expect(dataUrl.length).toBeGreaterThan(0x8000);
    });
});
