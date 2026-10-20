import { Clock } from 'lucide-react';
import { memo } from 'react';

import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { formatDuration } from '@/lib/utils/format';

// Colour per plan step family. The vocabulary itself lives in
// backend/pkg/tools/subtask_metadata.go and to-implement/PLAN_extended-version.md § 3.9;
// an unknown value falls back to a neutral outline badge.
const TYPE_VARIANTS: Record<string, BadgeVariant> = {
    analysis: 'purple',
    coding: 'secondary',
    collection: 'orange',
    'credential-access': 'red',
    enumeration: 'blue',
    exfiltration: 'orange',
    exploitation: 'red',
    'lateral-movement': 'orange',
    persistence: 'orange',
    'privilege-escalation': 'orange',
    recon: 'blue',
    reporting: 'secondary',
    research: 'purple',
    setup: 'secondary',
    verification: 'purple',
    'vulnerability-analysis': 'purple',
    'web-testing': 'red',
};

interface FlowSubtaskBadgesProps {
    budgetSeconds?: null | number;
    evidence?: null | string[];
    type?: null | string;
}

function FlowSubtaskBadges({ budgetSeconds, evidence, type }: FlowSubtaskBadgesProps) {
    if (!type && !evidence?.length && !budgetSeconds) {
        return null;
    }

    return (
        <div className="flex flex-wrap items-center gap-1">
            {type && <Badge variant={TYPE_VARIANTS[type] ?? 'outline'}>{type}</Badge>}
            {evidence?.map((item) => (
                <Badge
                    className="font-mono text-[10px] font-medium"
                    key={item}
                    variant="outline"
                >
                    {item}
                </Badge>
            ))}
            {!!budgetSeconds && (
                <Badge variant="secondary">
                    <Clock className="size-3" /> budget {formatDuration(budgetSeconds)}
                </Badge>
            )}
        </div>
    );
}

export default memo(FlowSubtaskBadges);
