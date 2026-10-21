import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import type { ReportDownloadFormat } from './report-download';

import FlowReportActions from './flow-report-actions';

interface SetupProps {
    canDelete?: boolean;
    canDownload?: boolean;
    downloading?: boolean;
}

const setup = ({ canDelete = true, canDownload = true, downloading = false }: SetupProps = {}) => {
    const onDelete = vi.fn();
    const onDownload = vi.fn<(format: ReportDownloadFormat) => void>();

    render(
        <FlowReportActions
            canDelete={canDelete}
            canDownload={canDownload}
            downloading={downloading}
            onDelete={onDelete}
            onDownload={onDownload}
        />,
    );

    return { onDelete, onDownload, trigger: screen.getByRole('button', { name: 'Report actions' }) };
};

describe('FlowReportActions', () => {
    it('lists the four export formats and hands the picked one back', async () => {
        const user = userEvent.setup();
        const { onDownload, trigger } = setup();

        await user.click(trigger);

        expect(screen.getByRole('menuitem', { name: 'Markdown' })).toBeInTheDocument();
        expect(screen.getByRole('menuitem', { name: 'PDF' })).toBeInTheDocument();
        expect(screen.getByRole('menuitem', { name: 'Word (DOCX)' })).toBeInTheDocument();
        expect(screen.getByRole('menuitem', { name: /Archive \(ZIP\)/ })).toBeInTheDocument();

        await user.click(screen.getByRole('menuitem', { name: 'PDF' }));

        expect(onDownload).toHaveBeenCalledWith('pdf');
    });

    it('disables the exports while the run is still writing', async () => {
        const user = userEvent.setup();
        const { trigger } = setup({ canDownload: false });

        await user.click(trigger);

        expect(screen.getByRole('menuitem', { name: 'Markdown' })).toHaveAttribute('aria-disabled', 'true');
        expect(screen.getByRole('menuitem', { name: 'PDF' })).toHaveAttribute('aria-disabled', 'true');
    });

    it('offers the delete action behind a separator when allowed', async () => {
        const user = userEvent.setup();
        const { onDelete, trigger } = setup();

        await user.click(trigger);
        await user.click(screen.getByRole('menuitem', { name: 'Delete report' }));

        expect(onDelete).toHaveBeenCalledTimes(1);
    });

    it('hides the delete action without the right', async () => {
        const user = userEvent.setup();
        const { trigger } = setup({ canDelete: false });

        await user.click(trigger);

        expect(screen.queryByRole('menuitem', { name: 'Delete report' })).not.toBeInTheDocument();
    });

    it('swaps the ellipsis for a spinner while a download is running', () => {
        const { trigger } = setup({ downloading: true });

        expect(trigger.querySelector('svg')).toHaveClass('animate-spin');
    });
});
