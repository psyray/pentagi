import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { ReportSectionFragmentFragment } from '@/graphql/types';

import type { ReportSectionEditor } from './use-flow-report-editor';

import FlowReportAttachments from './flow-report-attachments';

type ReportAttachment = ReportSectionFragmentFragment['attachments'][number];

const attachment = (overrides: Partial<ReportAttachment> & { id: string }): ReportAttachment => ({
    bytes: 0,
    createdAt: '2026-10-08T00:00:00Z',
    label: `file-${overrides.id}`,
    path: `uploads/file-${overrides.id}`,
    ...overrides,
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

describe('FlowReportAttachments', () => {
    it('renders a downloadable attachment and removes it through the editor', () => {
        const instance = editor();

        render(
            <FlowReportAttachments
                attachments={[attachment({ bytes: 2048, id: '1', label: 'report.pdf', path: 'uploads/report.pdf' })]}
                canEdit
                editor={instance}
                flowId="5"
                sectionId="10"
            />,
        );

        const link = screen.getByRole('link', { name: /report\.pdf/ });

        expect(link).toHaveAttribute('href', expect.stringContaining('/flows/5/files/download?'));
        expect(screen.getByText('2 KB')).toBeInTheDocument();

        fireEvent.click(screen.getByRole('button', { name: 'Remove report.pdf' }));

        expect(instance.setAttachments).toHaveBeenCalledWith('10', []);
    });

    it('renders an absolute container path without a download link', () => {
        render(
            <FlowReportAttachments
                attachments={[attachment({ bytes: 10, id: '2', label: 'notes.txt', path: '/work/notes.txt' })]}
                canEdit={false}
                editor={editor()}
                flowId="5"
                sectionId="10"
            />,
        );

        expect(screen.queryByRole('link')).not.toBeInTheDocument();
        expect(screen.getByText('notes.txt')).toBeInTheDocument();
    });

    it('renders nothing without attachments nor the edit right', () => {
        const { container } = render(
            <FlowReportAttachments
                attachments={[]}
                canEdit={false}
                editor={editor()}
                flowId="5"
                sectionId="10"
            />,
        );

        expect(container).toBeEmptyDOMElement();
    });
});
