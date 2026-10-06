import { formatDuration, formatTokenCount } from '@/lib/utils/format';

import type { GroupMetrics } from './flow-automation-groups';

interface FlowAutomationSummaryProps {
    className?: string;
    metrics: GroupMetrics;
}

function FlowAutomationSummary({ className, metrics }: FlowAutomationSummaryProps) {
    const parts = [`${metrics.messages} message${metrics.messages === 1 ? '' : 's'}`];

    if (metrics.durationSeconds > 0) {
        parts.push(formatDuration(metrics.durationSeconds));
    }

    if (metrics.tokens > 0) {
        parts.push(formatTokenCount(metrics.tokens));
    }

    return <span className={className}>{parts.join(' · ')}</span>;
}

export default FlowAutomationSummary;
