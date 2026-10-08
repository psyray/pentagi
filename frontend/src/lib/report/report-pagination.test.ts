import { describe, expect, it } from 'vitest';

import { headingKind, ReportPaginator } from './report-pagination';

describe('headingKind', () => {
    it('classifies a heading by its depth and number', () => {
        expect(headingKind(1, 'Paperwork report')).toBe('title');
        expect(headingKind(2, 'Cover Page')).toBe('front');
        expect(headingKind(2, '7. Detailed Findings')).toBe('chapter');
        expect(headingKind(2, '7.1. Weak TLS')).toBe('subsection');
        expect(headingKind(3, 'What it takes to implement')).toBe('content');
    });
});

describe('ReportPaginator', () => {
    const plan = (headings: [number, string][]) => {
        const paginator = new ReportPaginator();

        return headings.map(([level, text]) => paginator.heading(level, text));
    };

    it('breaks before every chapter but the cover, and every finding but the first of its chapter', () => {
        const decisions = plan([
            [1, 'Paperwork report'],
            [2, 'Cover Page'],
            [2, 'Limitations and Disclaimer'],
            [2, '1. Executive Summary'],
            [2, '7. Detailed Findings'],
            [2, '7.1. Weak TLS'],
            [2, '7.2. SQL Injection'],
            [2, '8. Recommendations'],
            [2, '8.1. Patch the gateway'],
        ]);

        expect(decisions.map((decision) => decision.breakBefore)).toEqual([
            false,
            false,
            true,
            true,
            true,
            false,
            true,
            true,
            false,
        ]);
        // The table of contents sits right before the second level-2 heading.
        expect(decisions.map((decision) => decision.insertTocBefore)).toEqual([
            false,
            false,
            true,
            false,
            false,
            false,
            false,
            false,
            false,
        ]);
    });

    it('lets a chapter body settle it, so its first finding still opens a page', () => {
        const paginator = new ReportPaginator();

        paginator.heading(2, '7. Detailed Findings');
        paginator.content();

        expect(paginator.heading(2, '7.1. Weak TLS').breakBefore).toBe(true);
    });
});
