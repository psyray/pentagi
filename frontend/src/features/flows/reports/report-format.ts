import { ReportSectionStatusType, ReportStatusType } from '@/graphql/types';

export const formatDuration = (seconds: number): string => {
    if (!Number.isFinite(seconds) || seconds < 0) {
        return '-';
    }

    if (seconds < 60) {
        return `${Math.round(seconds)}s`;
    }

    const totalMinutes = Math.floor(seconds / 60);
    const remainingSeconds = Math.round(seconds % 60);

    if (totalMinutes < 60) {
        return remainingSeconds > 0 ? `${totalMinutes}m ${remainingSeconds}s` : `${totalMinutes}m`;
    }

    const hours = Math.floor(totalMinutes / 60);
    const minutes = totalMinutes % 60;

    return minutes > 0 ? `${hours}h ${minutes}m` : `${hours}h`;
};

export const formatTokens = (tokens: number): string => {
    if (tokens >= 1_000_000) {
        return `${(tokens / 1_000_000).toFixed(2)}M`;
    }

    if (tokens >= 1_000) {
        return `${(tokens / 1_000).toFixed(1)}K`;
    }

    return `${tokens}`;
};

// A run is active while the worker still owns it: created, planning, generating.
export const ACTIVE_REPORT_STATUSES: readonly ReportStatusType[] = [
    ReportStatusType.Created,
    ReportStatusType.Planning,
    ReportStatusType.Generating,
];

export const isReportActive = (status: ReportStatusType): boolean => ACTIVE_REPORT_STATUSES.includes(status);

// The report-card Resume affordance works off the row alone: a failed or
// canceled report can always be resumed; a completed one is only resumable when
// it still holds unwritten sections, which the list row does not carry. The full
// document page knows the sections and uses isSectionResumable for a precise
// answer instead.
export const isReportResumable = (report: { status: ReportStatusType }): boolean =>
    report.status === ReportStatusType.Failed || report.status === ReportStatusType.Canceled;

// A section the writer still owes content for: resume / write-one act on these.
export const isSectionResumable = (status: ReportSectionStatusType): boolean =>
    status === ReportSectionStatusType.Queued ||
    status === ReportSectionStatusType.NotWritten ||
    status === ReportSectionStatusType.Failed;

const REPORT_STATUS_LABELS: Record<ReportStatusType, string> = {
    [ReportStatusType.Canceled]: 'Canceled',
    [ReportStatusType.Completed]: 'Completed',
    [ReportStatusType.Created]: 'Created',
    [ReportStatusType.Failed]: 'Failed',
    [ReportStatusType.Generating]: 'Generating',
    [ReportStatusType.Planning]: 'Planning',
};

export const reportStatusLabel = (status: ReportStatusType): string => REPORT_STATUS_LABELS[status] ?? status;

const SECTION_STATUS_LABELS: Record<ReportSectionStatusType, string> = {
    [ReportSectionStatusType.Done]: 'Done',
    [ReportSectionStatusType.Failed]: 'Failed',
    [ReportSectionStatusType.NotWritten]: 'Not written',
    [ReportSectionStatusType.Queued]: 'Queued',
    [ReportSectionStatusType.Skipped]: 'Skipped',
    [ReportSectionStatusType.Writing]: 'Being written',
};

export const sectionStatusLabel = (status: ReportSectionStatusType): string => SECTION_STATUS_LABELS[status] ?? status;

// formatBytes renders a content size the way the report card shows it: coarse,
// UTF-8 aware, never noisy (1 KB rather than 1.0 KB).
const encoder = new TextEncoder();

export const formatBytes = (text: string): string => {
    const bytes = encoder.encode(text).length;

    if (bytes < 1024) {
        return `${bytes} B`;
    }

    if (bytes < 1024 * 1024) {
        return `${Math.round(bytes / 1024)} KB`;
    }

    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
};
