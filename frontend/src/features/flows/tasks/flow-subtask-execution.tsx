import { memo } from 'react';

import type { SubtaskExecutionStatsFragmentFragment } from '@/graphql/types';

import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { StatusType } from '@/graphql/types';
import { formatCost, formatDuration, formatNumber, formatTokenCount } from '@/lib/utils/format';

interface FlowSubtaskExecutionProps {
    budgetSeconds?: null | number;
    stats?: SubtaskExecutionStatsFragmentFragment;
    status: StatusType;
}

const STATUS_LABELS: Record<StatusType, string> = {
    [StatusType.Created]: 'pending',
    [StatusType.Failed]: 'failed',
    [StatusType.Finished]: 'succeeded',
    [StatusType.Running]: 'running',
    [StatusType.Waiting]: 'waiting',
};

const STATUS_VARIANTS: Record<StatusType, BadgeVariant> = {
    [StatusType.Created]: 'secondary',
    [StatusType.Failed]: 'red',
    [StatusType.Finished]: 'green',
    [StatusType.Running]: 'blue',
    [StatusType.Waiting]: 'yellow',
};

interface Metric {
    label: string;
    value: string;
}

function FlowSubtaskExecution({ budgetSeconds, stats, status }: FlowSubtaskExecutionProps) {
    const metrics: Metric[] = [];

    if (stats) {
        const elapsed = formatDuration(stats.totalDurationSeconds);
        metrics.push({
            label: 'Elapsed',
            value: budgetSeconds ? `${elapsed} / ${formatDuration(budgetSeconds)}` : elapsed,
        });

        const toolcalls = stats.totalToolcallsCount;
        const avgToolcall = toolcalls > 0 ? stats.totalToolcallsDurationSeconds / toolcalls : 0;
        metrics.push({
            label: 'Tool calls',
            value: toolcalls > 0 ? `${formatNumber(toolcalls)} · ${formatDuration(avgToolcall)}` : '0',
        });

        metrics.push({ label: 'Sub-agents', value: formatNumber(stats.subAgentsCount) });
        metrics.push({
            label: 'Tokens in / out',
            value: `${formatTokenCount(stats.totalUsageIn)} / ${formatTokenCount(stats.totalUsageOut)}`,
        });
        metrics.push({
            label: 'Cache in / out',
            value: `${formatTokenCount(stats.totalUsageCacheIn)} / ${formatTokenCount(stats.totalUsageCacheOut)}`,
        });
        metrics.push({ label: 'Cost', value: formatCost(stats.totalCostIn + stats.totalCostOut) });
    }

    return (
        <Card>
            <CardHeader className="p-3">
                <CardTitle className="flex items-center gap-2">
                    Execution
                    <Badge variant={STATUS_VARIANTS[status]}>{STATUS_LABELS[status]}</Badge>
                </CardTitle>
            </CardHeader>
            <CardContent className="p-3 pt-0">
                <hr className="mt-0 mb-3" />
                {stats ? (
                    <div className="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3">
                        {metrics.map((metric) => (
                            <div
                                className="flex flex-col"
                                key={metric.label}
                            >
                                <span className="text-muted-foreground text-xs">{metric.label}</span>
                                <span className="text-sm font-medium">{metric.value}</span>
                            </div>
                        ))}
                    </div>
                ) : (
                    <div className="text-muted-foreground text-xs">No execution metrics yet.</div>
                )}
            </CardContent>
        </Card>
    );
}

export default memo(FlowSubtaskExecution);
