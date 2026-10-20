import type { ReactElement } from 'react';

import { describe, expect, it } from 'vitest';

import { pdfStyles, renderTextWithCJK, splitByCJK } from './report-pdf';

describe('pdf paragraph leading', () => {
    // react-pdf lays a Text's line box out with its own default font size when the
    // Text inherits the page's, which doubled the paragraph leading against the
    // lists. The explicit size on the paragraph is what keeps them equal.
    it('sets its own font size so a paragraph is as tight as a list item', () => {
        expect(pdfStyles.paragraph.fontSize).toBe(pdfStyles.listContent.fontSize);
        expect(pdfStyles.paragraph.lineHeight).toBe(pdfStyles.listContent.lineHeight);
    });
});

type CJKElement = ReactElement<{ style: Record<string, string | undefined> }>;

describe('splitByCJK', () => {
    it('returns a single non-CJK segment for plain Latin text', () => {
        expect(splitByCJK('hello world')).toEqual([{ isCJK: false, text: 'hello world' }]);
    });

    it('returns a single CJK segment for Chinese text', () => {
        expect(splitByCJK('中文')).toEqual([{ isCJK: true, text: '中文' }]);
    });

    it('splits mixed Latin and CJK into alternating segments', () => {
        expect(splitByCJK('Hello 世界 ok')).toEqual([
            { isCJK: false, text: 'Hello ' },
            { isCJK: true, text: '世界' },
            { isCJK: false, text: ' ok' },
        ]);
    });

    it('detects Japanese kana as CJK', () => {
        expect(splitByCJK('あいうカ')).toEqual([{ isCJK: true, text: 'あいうカ' }]);
    });

    it('detects Korean hangul as CJK even though NotoSansSC cannot render it', () => {
        expect(splitByCJK('한국어')).toEqual([{ isCJK: true, text: '한국어' }]);
    });

    it('treats CJK punctuation as CJK', () => {
        expect(splitByCJK('「x」')).toEqual([
            { isCJK: true, text: '「' },
            { isCJK: false, text: 'x' },
            { isCJK: true, text: '」' },
        ]);
    });

    it('falls back to a single empty non-CJK segment for empty input', () => {
        expect(splitByCJK('')).toEqual([{ isCJK: false, text: '' }]);
    });
});

describe('renderTextWithCJK', () => {
    it('returns the raw string when there is no CJK to split', () => {
        expect(renderTextWithCJK('plain text', 'Base', 'Bold', 'k')).toBe('plain text');
    });

    it('renders CJK on NotoSansSC and Latin on the base family', () => {
        const out = renderTextWithCJK('A中', 'Base', 'Bold', 'k') as CJKElement[];
        expect(out.map((element) => element.props.style.fontFamily)).toEqual(['Base', 'NotoSansSC']);
    });

    it('bolds only the Latin segment and keeps CJK on NotoSansSC when bold', () => {
        const out = renderTextWithCJK('A中', 'Base', 'Bold', 'k', true) as CJKElement[];
        expect(out[0]?.props.style).toMatchObject({ fontFamily: 'Bold', fontWeight: 'bold' });
        expect(out[1]?.props.style).toEqual({ fontFamily: 'NotoSansSC' });
    });
});
