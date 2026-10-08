import { describe, expect, it } from 'vitest';

import { parseReportMarkdown, reportTocEntries } from './report-docx';

describe('parseReportMarkdown', () => {
    it('maps headings and inline emphasis', () => {
        const blocks = parseReportMarkdown('## 1. Findings\n\nA **bold** and `code` run.\n');

        expect(blocks[0]).toEqual({ level: 2, text: '1. Findings', type: 'heading' });
        expect(blocks[1]).toEqual({
            runs: [
                { text: 'A ' },
                { bold: true, text: 'bold' },
                { text: ' and ' },
                { code: true, text: 'code' },
                { text: ' run.' },
            ],
            type: 'paragraph',
        });
    });

    it('maps ordered and unordered lists', () => {
        expect(parseReportMarkdown('- one\n- two\n')).toEqual([
            { items: [[{ text: 'one' }], [{ text: 'two' }]], ordered: false, type: 'list' },
        ]);
        expect(parseReportMarkdown('1. first\n2. second\n')).toEqual([
            { items: [[{ text: 'first' }], [{ text: 'second' }]], ordered: true, type: 'list' },
        ]);
    });

    it('maps a GFM table to header and rows', () => {
        const blocks = parseReportMarkdown('| Term | Explanation |\n|---|---|\n| TLS | Transport Layer Security |\n');

        expect(blocks).toEqual([
            { header: ['Term', 'Explanation'], rows: [['TLS', 'Transport Layer Security']], type: 'table' },
        ]);
    });

    it('keeps fenced code as a code block', () => {
        const blocks = parseReportMarkdown('```\nuid=0(root)\n```\n');

        expect(blocks[0]?.type).toBe('code');
        expect((blocks[0] as { text: string }).text).toContain('uid=0(root)');
    });
});

describe('reportTocEntries', () => {
    it('lists chapters at level 1 and findings at level 2, skipping the cover and body headings', () => {
        const blocks = parseReportMarkdown(
            [
                '# Paperwork report',
                '## Cover Page',
                'Cover text.',
                '## Limitations and Disclaimer',
                'text',
                '## 1. Executive Summary',
                'text',
                '## 7. Detailed Findings',
                '## 7.1. Weak TLS',
                '### What it takes to implement',
                'text',
                '## 7.2. SQL Injection',
                '## 8. Recommendations',
                '## 8.1. Patch the gateway',
                '## Page Header and Footer',
            ].join('\n\n'),
        );

        expect(reportTocEntries(blocks)).toEqual([
            { level: 1, title: 'Limitations and Disclaimer' },
            { level: 1, title: '1. Executive Summary' },
            { level: 1, title: '7. Detailed Findings' },
            { level: 2, title: '7.1. Weak TLS' },
            { level: 2, title: '7.2. SQL Injection' },
            { level: 1, title: '8. Recommendations' },
            { level: 2, title: '8.1. Patch the gateway' },
            { level: 1, title: 'Page Header and Footer' },
        ]);
    });
});
