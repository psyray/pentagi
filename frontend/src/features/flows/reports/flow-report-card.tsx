import { CheckCircle2, CircleSlash, Download, ExternalLink, TrendingUp, XCircle } from 'lucide-react';
import { useEffect, useState } from 'react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/lib/utils';

import type { ReportProgress } from './use-flow-reports';

import { formatDuration, formatTokens, isReportActive } from './report-format';

const TICK_INTERVAL_MS = 15_000;

const isRunActive = (report: ReportProgress): boolean => isReportActive(report.status);

const statusIconClass = (report: ReportProgress): string => {
    switch (report.status) {
        case 'canceled':
        case 'failed':
            return 'text-red-500';
        case 'completed':
            return 'text-emerald-500';
        default:
            return 'text-primary';
    }
};

interface FlowReportCardProps {
    onDownload: (report: ReportProgress) => void;
    onOpen: (report: ReportProgress) => void;
    report: ReportProgress;
}

function FlowReportCard({ onDownload, onOpen, report }: FlowReportCardProps) {
    const active = isRunActive(report);
    const [now, setNow] = useState(() => Date.now());

    useEffect(() => {
        if (!active) {
            return;
        }

        const interval = setInterval(() => setNow(Date.now()), TICK_INTERVAL_MS);

        return () => clearInterval(interval);
    }, [active]);

    const startedAt = new Date(report.createdAt).getTime();
    const tookSeconds = report.finishedAt
        ? (new Date(report.finishedAt).getTime() - startedAt) / 1000
        : Math.max(0, (now - startedAt) / 1000);

    const budgetSeconds = (report.controlBudgetSeconds || 0) + (report.contentBudgetSeconds || 0);
    const progress =
        report.sectionsTotal > 0 ? Math.min(100, Math.round((report.sectionsDone / report.sectionsTotal) * 100)) : 0;
    const activeSection = active && report.sectionsTotal > report.sectionsDone;

    const counter = active
        ? report.sectionsTotal > 0
            ? `${report.sectionsDone} of ${report.sectionsTotal} sections`
            : report.sectionsEstimate > 0
              ? `${report.sectionsEstimate} sections (estimating)`
              : 'Planning outline'
        : `${report.sectionsDone} of ${report.sectionsTotal} sections`;

    return (
        <div className="bg-card rounded-lg border p-4">
            <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                    <div className="flex min-w-0 items-center gap-2">
                        {reportStatusIcon(report)}
                        <span className="truncate font-medium">{report.title || 'Untitled report'}</span>
                        <Badge
                            className="shrink-0 font-mono text-xs"
                            variant="secondary"
                        >
                            {report.templateKey.replace(/^.*\//, '')}
                        </Badge>
                    </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                    <Button
                        aria-label="Open report"
                        onClick={() => onOpen(report)}
                        size="icon"
                        title="Open report"
                        variant="ghost"
                    >
                        <ExternalLink />
                    </Button>
                    <Button
                        aria-label="Download report"
                        disabled={active || report.status !== 'completed'}
                        onClick={() => onDownload(report)}
                        size="icon"
                        title={active ? 'Report has not finished generating yet' : 'Download report'}
                        variant="ghost"
                    >
                        <Download />
                    </Button>
                </div>
            </div>

            <div className="mt-3 flex items-center gap-3">
                <div className="bg-muted relative h-1.5 min-w-0 flex-1 overflow-hidden rounded-full">
                    <div
                        className={cn('h-full rounded-full transition-all', active ? 'bg-primary/70' : 'bg-primary')}
                        style={{ width: `${progress}%` }}
                    />
                    {active && (
                        <div
                            aria-hidden
                            className="pointer-events-none absolute inset-0 bg-[repeating-linear-gradient(45deg,transparent,transparent_6px,rgba(255,255,255,0.18)_6px,rgba(255,255,255,0.18)_12px)]"
                        />
                    )}
                </div>
                <div className="text-muted-foreground shrink-0 text-right text-xs">
                    <div className="whitespace-nowrap">{counter}</div>
                    <div className="flex items-center justify-end gap-1 whitespace-nowrap">
                        <TrendingUp className="size-3" />
                        <span>
                            {formatDuration(tookSeconds)}
                            {budgetSeconds > 0 && ` of ${formatDuration(budgetSeconds)}`}
                            {activeSection && report.tokensIn + report.tokensOut > 0
                                ? ` · ${formatTokens(report.tokensIn + report.tokensOut)} tok`
                                : ''}
                        </span>
                    </div>
                </div>
            </div>

            {report.error && <div className="mt-2 text-xs text-red-500">{report.error}</div>}
        </div>
    );
}

function reportStatusIcon(report: ReportProgress) {
    if (isRunActive(report)) {
        return <Spinner className={cn('size-4', statusIconClass(report))} />;
    }

    switch (report.status) {
        case 'completed':
            return <CheckCircle2 className={cn('size-4', statusIconClass(report))} />;
        case 'failed':
            return <XCircle className={cn('size-4', statusIconClass(report))} />;
        default:
            return <CircleSlash className={cn('size-4', statusIconClass(report))} />;
    }
}

export default FlowReportCard;
