import { describe, expect, it } from 'vitest';

import { type ReportSectionFragmentFragment, ReportSectionKind, ReportSectionStatusType } from '@/graphql/types';

import {
    collectSubtreeIds,
    parseVariableRows,
    sectionVariablesDiffer,
    serializeVariables,
    siblingMoveBoundaries,
} from './report-section-edit';

const section = (
    overrides: Partial<ReportSectionFragmentFragment> & { id: string },
): ReportSectionFragmentFragment => ({
    attachments: [],
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

describe('serializeVariables', () => {
    it('drops unnamed rows and trims names', () => {
        expect(
            serializeVariables([
                { name: '  Port  ', value: '443' },
                { name: '   ', value: 'ignored' },
            ]),
        ).toBe('[{"name":"Port","value":"443"}]');
    });

    it('keeps an empty value as an empty string, never null', () => {
        expect(serializeVariables([{ name: 'Host', value: '' }])).toBe('[{"name":"Host","value":""}]');
    });

    it('round-trips through parseVariableRows', () => {
        const rows = [
            { name: 'A', value: '1' },
            { name: 'B', value: 'text' },
        ];

        expect(parseVariableRows(JSON.parse(serializeVariables(rows)))).toEqual(rows);
    });

    it('treats a missing variables list as empty', () => {
        expect(parseVariableRows(null)).toEqual([]);
        expect(parseVariableRows(undefined)).toEqual([]);
    });
});

describe('sectionVariablesDiffer', () => {
    it('is false when the rows match the stored values', () => {
        expect(sectionVariablesDiffer([{ name: 'A', value: '1' }], [{ name: 'A', value: '1' }])).toBe(false);
    });

    it('is true when a name, a value, or the order changed', () => {
        expect(sectionVariablesDiffer([{ name: 'A', value: '2' }], [{ name: 'A', value: '1' }])).toBe(true);
        expect(sectionVariablesDiffer([{ name: 'A', value: '1' }], [])).toBe(true);
        expect(
            sectionVariablesDiffer(
                [
                    { name: 'B', value: '2' },
                    { name: 'A', value: '1' },
                ],
                [
                    { name: 'A', value: '1' },
                    { name: 'B', value: '2' },
                ],
            ),
        ).toBe(true);
    });
});

describe('siblingMoveBoundaries', () => {
    const siblings = [
        section({ id: '1', position: 0 }),
        section({ id: '2', position: 1 }),
        section({ id: '3', position: 2 }),
    ];

    it('blocks the move up on the first sibling and down on the last', () => {
        expect(siblingMoveBoundaries(siblings, '1')).toEqual({ canMoveDown: true, canMoveUp: false });
        expect(siblingMoveBoundaries(siblings, '3')).toEqual({ canMoveDown: false, canMoveUp: true });
    });

    it('allows both moves in the middle', () => {
        expect(siblingMoveBoundaries(siblings, '2')).toEqual({ canMoveDown: true, canMoveUp: true });
    });

    it('only counts sections under the same parent', () => {
        const nested = [
            section({ id: '1', position: 0 }),
            section({ id: '2', parentId: '1', position: 0 }),
            section({ id: '3', parentId: '1', position: 1 }),
            section({ id: '4', position: 1 }),
        ];

        expect(siblingMoveBoundaries(nested, '2')).toEqual({ canMoveDown: true, canMoveUp: false });
        expect(siblingMoveBoundaries(nested, '4')).toEqual({ canMoveDown: false, canMoveUp: true });
    });

    it('keeps hidden siblings in the count, mirroring the server', () => {
        const withHidden = [section({ id: '1', position: 0 }), section({ hidden: true, id: '2', position: 1 })];

        expect(siblingMoveBoundaries(withHidden, '2')).toEqual({ canMoveDown: false, canMoveUp: true });
    });

    it('never matches an unknown id', () => {
        expect(siblingMoveBoundaries(siblings, '99')).toEqual({ canMoveDown: false, canMoveUp: false });
    });
});

describe('collectSubtreeIds', () => {
    it('returns the section and every descendant, depth first', () => {
        const tree = [
            section({ id: '1' }),
            section({ id: '2', parentId: '1' }),
            section({ id: '3', parentId: '2' }),
            section({ id: '4', parentId: '1' }),
            section({ id: '5' }),
        ];

        expect(collectSubtreeIds(tree, '1').sort()).toEqual(['1', '2', '3', '4']);
        expect(collectSubtreeIds(tree, '5')).toEqual(['5']);
    });

    it('handles a leaf and a missing id', () => {
        expect(collectSubtreeIds([section({ id: '1' })], '1')).toEqual(['1']);
        expect(collectSubtreeIds([section({ id: '1' })], '99')).toEqual([]);
    });
});
