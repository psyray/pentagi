import { describe, expect, it } from 'vitest';

import {
    type ReportLogFragmentFragment,
    ReportLogKind,
    type ReportSectionFragmentFragment,
    ReportSectionKind,
    ReportSectionStatusType,
} from '@/graphql/types';

import { groupReportLogs, groupTitle, logSectionId, REPORT_GROUP_KEY } from './flow-reporting-groups';

const log = (overrides: Partial<ReportLogFragmentFragment> & { id: string }): ReportLogFragmentFragment => ({
    createdAt: '2026-10-08T00:00:00Z',
    kind: ReportLogKind.Section,
    message: `log ${overrides.id}`,
    payload: null,
    reportId: '1',
    sectionId: null,
    ...overrides,
});

const section = (id: string, title: string): ReportSectionFragmentFragment => ({
    content: '',
    createdAt: '2026-10-08T00:00:00Z',
    durationSeconds: 0,
    hidden: false,
    id,
    instructions: null,
    key: `section-${id}`,
    kind: ReportSectionKind.Llm,
    parentId: null,
    position: 0,
    reportId: '1',
    status: ReportSectionStatusType.Done,
    title,
    tokensIn: 0,
    tokensOut: 0,
    updatedAt: '2026-10-08T00:00:00Z',
    variables: [],
    warnings: null,
});

describe('logSectionId', () => {
    it('reads the section id from the payload', () => {
        expect(logSectionId(log({ id: '1', payload: { section_id: 42 } }))).toBe('42');
    });

    it('falls back to the column, which only carries operator notes', () => {
        expect(logSectionId(log({ id: '1', sectionId: '7' }))).toBe('7');
    });

    it('is null when neither carries an id', () => {
        expect(logSectionId(log({ id: '1', payload: { unrelated: true } }))).toBeNull();
        expect(logSectionId(log({ id: '1' }))).toBeNull();
    });
});

describe('groupReportLogs', () => {
    it('groups by section in first-appearance order and keeps ungrouped logs together', () => {
        const logs = [
            log({ id: '1', kind: ReportLogKind.Status, message: 'run started' }),
            log({ id: '2', payload: { section_id: 10 } }),
            log({ id: '3', payload: { section_id: 10 } }),
            log({ id: '4', payload: { section_id: 20 } }),
            log({ id: '5', kind: ReportLogKind.Note, message: 'a note' }),
        ];
        const sections = [section('10', 'Executive Summary'), section('20', 'Attack Chain')];

        const groups = groupReportLogs(logs, sections);

        expect(groups.map((group) => group.key)).toEqual([REPORT_GROUP_KEY, '10', '20']);
        expect(groups[0]?.logs.map((entry) => entry.id)).toEqual(['1', '5']);
        expect(groups[1]?.logs.map((entry) => entry.id)).toEqual(['2', '3']);
        expect(groups[1]?.title).toBe('Executive Summary');
        expect(groups[2]?.title).toBe('Attack Chain');
    });

    it('sorts the logs by id so a cache appending out of order still reads correctly', () => {
        const groups = groupReportLogs(
            [log({ id: '10', payload: { section_id: 1 } }), log({ id: '2', payload: { section_id: 1 } })],
            [section('1', 'One')],
        );

        expect(groups[0]?.logs.map((entry) => entry.id)).toEqual(['2', '10']);
    });

    it('tolerates a section the list does not know', () => {
        const groups = groupReportLogs([log({ id: '1', payload: { section_id: 99 } })], []);

        expect(groups[0]?.title).toBeNull();
        expect(groupTitle(groups[0]!)).toBe('Section 99');
    });

    it('names the ungrouped group and returns nothing for no logs', () => {
        const groups = groupReportLogs([log({ id: '1', kind: ReportLogKind.Status })], []);

        expect(groupTitle(groups[0]!)).toBe('Report run');
        expect(groupReportLogs([], [])).toEqual([]);
    });
});
