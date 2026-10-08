import type { ReactNode } from 'react';

import { useEffect, useState } from 'react';

import type { ReportFragmentFragment } from '@/graphql/types';

import { formatDuration, formatTokens, isReportActive, reportStatusLabel } from './report-format';

const TICK_INTERVAL_MS = 15_000;

interface FlowReportRunProps {
    report: ReportFragmentFragment;
}

function FlowReportRun({ report }: FlowReportRunProps) {
    const active = isReportActive(report.status);
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

    const budgetSeconds = report.controlBudgetSeconds + report.contentBudgetSeconds;
    const agentSeconds = report.controlUsedSeconds + report.contentUsedSeconds;

    return (
        <div className="bg-muted/40 flex flex-col gap-1.5 rounded-lg border p-3 text-xs">
            <div className="mb-1 font-medium tracking-wide uppercase">Run</div>
            <RunRow
                label="Status"
                value={reportStatusLabel(report.status)}
            />
            <RunRow
                label="Model"
                value={report.providerName || "The engagement's own"}
            />
            <RunRow
                label="Template"
                value={report.templateLabel || report.templateKey}
            />
            <RunRow
                label="Language"
                value={report.language}
            />
            <RunRow
                label="Took"
                value={
                    budgetSeconds > 0
                        ? `${formatDuration(tookSeconds)} of ${formatDuration(budgetSeconds)}`
                        : formatDuration(tookSeconds)
                }
            />
            <RunRow
                label="Agent time"
                value={formatDuration(agentSeconds)}
            />
            <RunRow
                label="Tokens"
                value={`${formatTokens(report.tokensIn)} in · ${formatTokens(report.tokensOut)} out`}
            />
        </div>
    );
}

function RunRow({ label, value }: { label: string; value: ReactNode }) {
    return (
        <div className="flex items-baseline justify-between gap-3">
            <span className="text-muted-foreground">{label}</span>
            <span className="truncate text-right">{value}</span>
        </div>
    );
}

export default FlowReportRun;
