import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { ReportFragmentFragment } from '@/graphql/types';

import { ProviderType, ReportStatusType } from '@/graphql/types';

const apollo = vi.hoisted(() => ({ client: {} }));
const pdf = vi.hoisted(() => ({ build: vi.fn(), fetch: vi.fn() }));

vi.mock('@apollo/client/react', () => ({ useApolloClient: () => apollo.client }));

vi.mock('./report-download', () => ({
    buildReportPdfBlob: (...args: unknown[]) => pdf.build(...args),
    fetchReportDocument: (...args: unknown[]) => pdf.fetch(...args),
}));

// PDF.js cannot render in jsdom; the mock reports three pages so the header shows
// the same count a real blob would and the page controls can be exercised.
vi.mock('react-pdf', async () => {
    const React = await import('react');

    return {
        Document: ({
            children,
            onLoadSuccess,
        }: {
            children: import('react').ReactNode;
            onLoadSuccess?: (info: { numPages: number }) => void;
        }) => {
            // Fire once, like PDF.js does on load: the component's callback is a
            // fresh closure each render and resets the page, so a naive effect
            // would fight the pager.
            const loaded = React.useRef(false);

            React.useEffect(() => {
                if (!loaded.current) {
                    loaded.current = true;
                    onLoadSuccess?.({ numPages: 3 });
                }
            });

            return <div data-slot="pdf-document">{children}</div>;
        },
        Page: ({ pageNumber }: { pageNumber: number }) => <div data-slot="pdf-page">{String(pageNumber)}</div>,
        pdfjs: { GlobalWorkerOptions: {} as { workerSrc?: string } },
    };
});

import FlowReportPreview from './flow-report-preview';

const report = (overrides: Partial<ReportFragmentFragment> = {}): ReportFragmentFragment => ({
    additionalInstructions: null,
    contentBudgetSeconds: 600,
    contentUsedSeconds: 88,
    controlBudgetSeconds: 120,
    controlUsedSeconds: 7,
    createdAt: '2026-10-08T00:00:00Z',
    error: null,
    finishedAt: '2026-10-08T00:05:00Z',
    flowId: '10',
    id: '1',
    language: 'English',
    providerName: 'OptimV2 glm-5.3-flash',
    providerType: ProviderType.Custom,
    sectionsDone: 25,
    sectionsEstimate: 25,
    sectionsTotal: 25,
    startedAt: '2026-10-08T00:00:00Z',
    status: ReportStatusType.Completed,
    templateKey: 'bundled/pentagi-default-pentest-report',
    templateLabel: 'PentAGI Penetration Testing Report',
    title: 'Paperwork report',
    tokensIn: 152974,
    tokensOut: 10454,
    updatedAt: '2026-10-08T00:05:00Z',
    userId: '1',
    ...overrides,
});

// The blob cache is module-scoped; a distinct id + revision per case keeps the
// tests from reading a neighbour's entry.
const setup = (id: string, updatedAt: string) =>
    render(
        <FlowReportPreview
            branding={null}
            report={report({ id, updatedAt })}
            reportId={id}
        />,
    );

const pdfBlob = () => new Blob(['%PDF'], { type: 'application/pdf' });

describe('FlowReportPreview', () => {
    beforeEach(() => {
        pdf.build.mockReset();
        pdf.fetch.mockReset();
    });

    it('builds the export blob and reports its page count', async () => {
        pdf.fetch.mockResolvedValue('the report body');
        pdf.build.mockResolvedValue(pdfBlob());

        setup('preview-count', '2026-10-10T10:00:00Z');

        expect(await screen.findByText('Page 1 / 3')).toBeInTheDocument();
        expect(pdf.build).toHaveBeenCalledWith(
            expect.objectContaining({ id: 'preview-count' }),
            'the report body',
            null,
        );
    });

    it('pages forward and backward through the document', async () => {
        const user = userEvent.setup();
        pdf.fetch.mockResolvedValue('the report body');
        pdf.build.mockResolvedValue(pdfBlob());

        setup('preview-paging', '2026-10-10T10:01:00Z');

        await screen.findByText('Page 1 / 3');

        await user.click(screen.getByRole('button', { name: 'Next page' }));
        expect(screen.getByText('Page 2 / 3')).toBeInTheDocument();

        await user.click(screen.getByRole('button', { name: 'Next page' }));
        expect(screen.getByText('Page 3 / 3')).toBeInTheDocument();

        expect(screen.getByRole('button', { name: 'Next page' })).toBeDisabled();
        expect(screen.getByRole('button', { name: 'Previous page' })).toBeEnabled();

        await user.click(screen.getByRole('button', { name: 'Previous page' }));
        expect(screen.getByText('Page 2 / 3')).toBeInTheDocument();
    });

    it('clamps the zoom at both ends', async () => {
        const user = userEvent.setup();
        pdf.fetch.mockResolvedValue('the report body');
        pdf.build.mockResolvedValue(pdfBlob());

        setup('preview-zoom', '2026-10-10T10:02:00Z');

        await screen.findByText('Page 1 / 3');

        await user.click(screen.getByRole('button', { name: 'Zoom in' }));
        expect(screen.getByRole('button', { name: 'Zoom in' })).toBeDisabled();

        await user.click(screen.getByRole('button', { name: 'Zoom out' }));
        await user.click(screen.getByRole('button', { name: 'Zoom out' }));
        expect(screen.getByRole('button', { name: 'Zoom out' })).toBeDisabled();
    });

    it('surfaces an empty document instead of rendering a blank preview', async () => {
        pdf.fetch.mockResolvedValue('   ');
        pdf.build.mockResolvedValue(pdfBlob());

        setup('preview-empty', '2026-10-10T10:03:00Z');

        expect(await screen.findByText('The report document is empty.')).toBeInTheDocument();
        expect(pdf.build).not.toHaveBeenCalled();
    });

    it('surfaces a build failure', async () => {
        pdf.fetch.mockResolvedValue('the report body');
        pdf.build.mockRejectedValue(new Error('pdf build exploded'));

        setup('preview-failure', '2026-10-10T10:04:00Z');

        expect(await screen.findByText('pdf build exploded')).toBeInTheDocument();
    });

    it('shows the build progress until the blob is ready', async () => {
        let resolveBuild: (blob: Blob) => void = () => {};

        pdf.fetch.mockResolvedValue('the report body');
        pdf.build.mockImplementation(
            () =>
                new Promise<Blob>((resolve) => {
                    resolveBuild = resolve;
                }),
        );

        setup('preview-loading', '2026-10-10T10:05:00Z');

        expect(await screen.findByText('Building the preview…')).toBeInTheDocument();

        resolveBuild(pdfBlob());

        await waitFor(() => expect(screen.queryByText('Building the preview…')).not.toBeInTheDocument());
    });
});
