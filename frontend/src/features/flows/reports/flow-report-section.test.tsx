import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { type ReportSectionFragmentFragment, ReportSectionKind, ReportSectionStatusType } from '@/graphql/types';

import type { ReportSectionEditor } from './use-flow-report-editor';

import FlowReportSection from './flow-report-section';
import { buildReportOutline } from './report-outline';

const section = (
    overrides: Partial<ReportSectionFragmentFragment> & { id: string },
): ReportSectionFragmentFragment => ({
    attachments: [],
    content: 'Body text',
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

const editor = (overrides: Partial<ReportSectionEditor> = {}): ReportSectionEditor => ({
    addSection: vi.fn().mockResolvedValue(true),
    busy: false,
    deleteSection: vi.fn().mockResolvedValue(true),
    moveSection: vi.fn().mockResolvedValue(true),
    saveSection: vi.fn().mockResolvedValue(true),
    setAttachments: vi.fn().mockResolvedValue(true),
    setSectionHidden: vi.fn().mockResolvedValue(true),
    writeSection: vi.fn().mockResolvedValue(true),
    ...overrides,
});

const renderSection = (sections: ReportSectionFragmentFragment[], canEdit = true, active = false) => {
    const outline = buildReportOutline(sections);

    return render(
        <FlowReportSection
            active={active}
            canEdit={canEdit}
            depth={0}
            editor={editorInstance}
            flowId={null}
            node={outline[0]!}
            sections={sections}
        />,
    );
};

let editorInstance: ReportSectionEditor;

describe('FlowReportSection', () => {
    it('renders the edit affordances and hides a section through the editor', () => {
        editorInstance = editor();
        renderSection([section({ id: '1' })]);

        expect(screen.getByRole('button', { name: 'Move section up' })).toBeDisabled();
        expect(screen.getByRole('button', { name: 'Move section down' })).toBeDisabled();

        fireEvent.click(screen.getByRole('button', { name: 'Hide section' }));

        expect(editorInstance.setSectionHidden).toHaveBeenCalledWith('1', true);
    });

    it('keeps the show control and the hidden badge on a hidden section', () => {
        editorInstance = editor();
        renderSection([section({ hidden: true, id: '1', title: 'Archived finding' })]);

        expect(screen.getByText('Hidden')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Show section' })).toBeInTheDocument();
    });

    it('renders no action without the edit right', () => {
        editorInstance = editor();
        renderSection([section({ id: '1' })], false);

        expect(screen.queryByRole('button', { name: 'Edit section' })).not.toBeInTheDocument();
    });

    it('keeps the edit affordance but disables structural actions while generating', () => {
        editorInstance = editor();
        renderSection([section({ id: '1' })], true, true);

        expect(screen.getByRole('button', { name: 'Edit section' })).toBeEnabled();
        expect(screen.getByRole('button', { name: 'Move section up' })).toBeDisabled();
        expect(screen.getByRole('button', { name: 'Add section' })).toBeDisabled();
        expect(screen.queryByRole('button', { name: 'Write section' })).not.toBeInTheDocument();
    });

    it('writes a section through the editor', () => {
        editorInstance = editor();
        renderSection([section({ id: '1' })]);

        fireEvent.click(screen.getByRole('button', { name: 'Write section' }));

        expect(editorInstance.writeSection).toHaveBeenCalledWith('1');
    });

    it('opens the inline editor on edit and closes it on cancel', () => {
        editorInstance = editor();
        renderSection([section({ id: '1' })]);

        fireEvent.click(screen.getByRole('button', { name: 'Edit section' }));

        expect(screen.getByLabelText('Title')).toHaveValue('Section 1');

        fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

        expect(screen.queryByLabelText('Title')).not.toBeInTheDocument();
    });

    it('asks for confirmation before deleting a section', () => {
        editorInstance = editor();
        renderSection([section({ id: '1' })]);

        fireEvent.click(screen.getByRole('button', { name: 'Delete section' }));
        fireEvent.click(screen.getByRole('button', { name: 'Delete' }));

        expect(editorInstance.deleteSection).toHaveBeenCalledWith('1');
    });

    it('shows the section variables in read mode', () => {
        editorInstance = editor();
        renderSection([
            section({
                id: '1',
                variables: [{ name: 'Port', value: '443' }],
            }),
        ]);

        expect(screen.getByText('Port')).toBeInTheDocument();
        expect(screen.getByText('443')).toBeInTheDocument();
    });
});
