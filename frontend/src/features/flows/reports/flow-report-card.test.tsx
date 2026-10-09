import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { ProviderType, ReportStatusType } from '@/graphql/types';

import FlowReportCard from './flow-report-card';
import { type ReportProgress } from './use-flow-reports';

const base: ReportProgress = {
    additionalInstructions: null,
    contentBudgetSeconds: 600,
    contentUsedSeconds: 88,
    controlBudgetSeconds: 120,
    controlUsedSeconds: 7,
    createdAt: new Date(Date.now() - 120_000).toISOString(),
    error: null,
    finishedAt: null,
    flowId: '10',
    id: '1',
    language: 'English',
    providerName: 'OptimV2 glm-5.3-flash',
    providerType: ProviderType.Custom,
    sectionsDone: 4,
    sectionsEstimate: 25,
    sectionsTotal: 25,
    startedAt: null,
    status: ReportStatusType.Generating,
    templateKey: 'bundled/pentagi-default-pentest-report',
    templateLabel: 'PentAGI Penetration Testing Report',
    title: 'Paperwork report',
    tokensIn: 152974,
    tokensOut: 10454,
    updatedAt: new Date().toISOString(),
    userId: '1',
};

const card = (overrides: Partial<ReportProgress> = {}, onDelete = vi.fn(() => Promise.resolve(true))) =>
    render(
        <FlowReportCard
            onDelete={onDelete}
            onDownload={() => {}}
            onOpen={() => {}}
            onResume={() => {}}
            report={{ ...base, ...overrides }}
        />,
    );

describe('FlowReportCard', () => {
    it('shows the running counters and the budget ceiling while generating', () => {
        card();

        expect(screen.getByText('Paperwork report')).toBeInTheDocument();
        expect(screen.getByText('4 of 25 sections')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Download report' })).toBeDisabled();
    });

    it('keeps download disabled until the run has finished', () => {
        card({ finishedAt: new Date().toISOString(), sectionsDone: 25, status: ReportStatusType.Completed });

        expect(screen.getByText('25 of 25 sections')).toBeInTheDocument();
        expect(screen.getByRole('button', { name: 'Download report' })).toBeEnabled();
    });

    it('shows the planning state before the planner has run', () => {
        card({ sectionsEstimate: 0, sectionsTotal: 0 });

        expect(screen.getByText('Planning outline')).toBeInTheDocument();
    });

    it('renders a failed run with its error line', () => {
        card({ error: 'planner call failed: boom', status: ReportStatusType.Failed });

        expect(screen.getByText('planner call failed: boom')).toBeInTheDocument();
    });

    it('exposes the sections expander on the card', () => {
        card();

        expect(screen.getByRole('button', { name: 'Expand the report sections' })).toBeInTheDocument();
    });

    it('deletes the report after the confirmation dialog is confirmed', async () => {
        const onDelete = vi.fn(() => Promise.resolve(true));
        card({ finishedAt: new Date().toISOString(), sectionsDone: 25, status: ReportStatusType.Completed }, onDelete);

        await userEvent.click(screen.getByRole('button', { name: 'Delete report' }));

        expect(await screen.findByText('Delete report')).toBeInTheDocument();

        await userEvent.click(screen.getByRole('button', { name: 'Delete' }));

        await waitFor(() => expect(onDelete).toHaveBeenCalledTimes(1));
        expect(onDelete).toHaveBeenCalledWith(expect.objectContaining({ id: '1' }));
    });
});
