import { describe, expect, it } from 'vitest';

import { type ReportSectionFragmentFragment, ReportSectionKind, ReportSectionStatusType } from '@/graphql/types';

import {
    buildReportOutline,
    findReportById,
    flattenReportOutline,
    numberedTitle,
    sameReportId,
} from './report-outline';

const section = (
    overrides: Partial<ReportSectionFragmentFragment> & { id: string },
): ReportSectionFragmentFragment => ({
    content: '',
    createdAt: '2026-10-08T00:00:00Z',
    durationSeconds: 0,
    hidden: false,
    instructions: null,
    key: `section-${overrides.id}`,
    kind: ReportSectionKind.Llm,
    parentId: null,
    position: 0,
    status: ReportSectionStatusType.Done,
    title: `Section ${overrides.id}`,
    tokensIn: 0,
    tokensOut: 0,
    updatedAt: '2026-10-08T00:00:00Z',
    variables: [],
    warnings: null,
    ...overrides,
    reportId: overrides.reportId ?? '1',
});

describe('buildReportOutline', () => {
    it('numbers top-level sections in document order and leaves front matter unnumbered', () => {
        const outline = buildReportOutline([
            section({ id: '1', key: 'cover-page', position: 0, title: 'Cover Page' }),
            section({ id: '2', key: 'limitations', position: 1, title: 'Limitations and Disclaimer' }),
            section({ id: '3', key: 'executive-summary', position: 2, title: 'Executive Summary' }),
            section({ id: '4', key: 'page-header-footer', position: 3, title: 'Page Header and Footer' }),
        ]);

        expect(outline.map(numberedTitle)).toEqual([
            'Cover Page',
            'Limitations and Disclaimer',
            '1. Executive Summary',
            'Page Header and Footer',
        ]);
    });

    it('numbers children under their parent and nests them in document order', () => {
        const outline = buildReportOutline([
            section({ id: '1', key: 'cover-page', position: 0, title: 'Cover Page' }),
            section({
                id: '2',
                key: 'detailed-findings',
                kind: ReportSectionKind.Aggregate,
                position: 1,
                title: 'Detailed Findings',
            }),
            section({ id: '3', parentId: '2', position: 2, title: 'Weak TLS' }),
            section({ id: '4', parentId: '2', position: 0, title: 'SQL injection' }),
            section({ id: '5', position: 3, title: 'Closing' }),
        ]);

        expect(outline.map(numberedTitle)).toEqual(['Cover Page', '1. Detailed Findings', '2. Closing']);
        expect(outline[1]?.children.map(numberedTitle)).toEqual(['1.1. SQL injection', '1.2. Weak TLS']);
        expect(flattenReportOutline(outline).map(numberedTitle)).toEqual([
            'Cover Page',
            '1. Detailed Findings',
            '1.1. SQL injection',
            '1.2. Weak TLS',
            '2. Closing',
        ]);
    });

    it('keeps children of an unnumbered front-matter section unnumbered', () => {
        const outline = buildReportOutline([
            section({ id: '1', key: 'cover-page', position: 0, title: 'Cover Page' }),
            section({ id: '2', parentId: '1', position: 0, title: 'Engagement fingerprint' }),
        ]);

        expect(outline[0]?.children.map(numberedTitle)).toEqual(['Engagement fingerprint']);
    });

    it('drops planner-skipped sections but keeps hidden ones, unnumbered', () => {
        const outline = buildReportOutline([
            section({ id: '1', position: 0, title: 'Kept' }),
            section({ hidden: true, id: '2', position: 1, title: 'Hidden' }),
            section({ id: '3', position: 2, status: ReportSectionStatusType.Skipped, title: 'Skipped' }),
            section({ id: '4', position: 3, title: 'Also kept' }),
        ]);

        // The hidden row stays reachable in the editor but takes no number, and
        // the following visible section keeps a contiguous number.
        expect(outline.map(numberedTitle)).toEqual(['1. Kept', 'Hidden', '2. Also kept']);
    });

    it('numbers visible children on their own counter, skipping hidden ones', () => {
        const outline = buildReportOutline([
            section({ id: '1', position: 0, title: 'Findings' }),
            section({ id: '2', parentId: '1', position: 0, title: 'First' }),
            section({ hidden: true, id: '3', parentId: '1', position: 1, title: 'Hidden child' }),
            section({ id: '4', parentId: '1', position: 2, title: 'Second' }),
        ]);

        expect(outline[0]?.children.map(numberedTitle)).toEqual(['1.1. First', 'Hidden child', '1.2. Second']);
    });

    it('sorts siblings by position regardless of the order the rows arrive in', () => {
        const outline = buildReportOutline([
            section({ id: '2', position: 5, title: 'Second' }),
            section({ id: '1', position: 1, title: 'First' }),
        ]);

        expect(outline.map(numberedTitle)).toEqual(['1. First', '2. Second']);
    });
});

describe('sameReportId / findReportById', () => {
    it('matches a numeric payload id against a string route param', () => {
        expect(sameReportId(8, '8')).toBe(true);
        expect(sameReportId('8', 8)).toBe(true);
        expect(sameReportId(8, '9')).toBe(false);
    });

    it('never matches a missing id', () => {
        expect(sameReportId(null, '8')).toBe(false);
        expect(sameReportId(8, undefined)).toBe(false);
    });

    it('finds the report whose integer id the URL names as text', () => {
        const reports = [
            { id: 8, title: 'Eight' },
            { id: 9, title: 'Nine' },
        ];

        expect(findReportById(reports, '9')?.title).toBe('Nine');
        expect(findReportById(reports, '10')).toBeNull();
    });
});
