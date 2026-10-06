import { ChevronDown } from 'lucide-react';
import { memo, useState } from 'react';

import type { BadgeVariant } from '@/components/ui/badge';
import type { StatusType } from '@/graphql/types';

import Markdown from '@/components/shared/markdown';
import { Badge } from '@/components/ui/badge';
import { StatusType as StatusTypeEnum } from '@/graphql/types';
import { cn } from '@/lib/utils';

import type { SubtaskGroup } from './flow-automation-groups';

import FlowAutomationAgentGroup from './flow-automation-agent-group';
import FlowAutomationSummary from './flow-automation-summary';
import FlowMessage from './flow-message';

const statusVariants: Record<StatusType, BadgeVariant> = {
    [StatusTypeEnum.Created]: 'blue',
    [StatusTypeEnum.Failed]: 'red',
    [StatusTypeEnum.Finished]: 'green',
    [StatusTypeEnum.Running]: 'purple',
    [StatusTypeEnum.Waiting]: 'yellow',
};

interface FlowAutomationGroupProps {
    group: SubtaskGroup;
    searchValue?: string;
}

function FlowAutomationGroup({ group, searchValue = '' }: FlowAutomationGroupProps) {
    const [isOpen, setIsOpen] = useState(true);
    const { subtask } = group;
    const title = subtask?.title || `Subtask ${group.subtaskId}`;

    return (
        <div className="bg-card text-card-foreground rounded-xl border shadow-sm">
            <div className="flex items-start gap-2 border-b p-3">
                <button
                    aria-expanded={isOpen}
                    aria-label="Toggle subtask"
                    className="text-muted-foreground hover:text-foreground mt-0.5 shrink-0"
                    onClick={() => setIsOpen(!isOpen)}
                    type="button"
                >
                    <ChevronDown className={cn('size-4 transition-transform', !isOpen && '-rotate-90')} />
                </button>
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <div className="flex items-center gap-2">
                        <Markdown className="prose-xs prose-fixed wrap-break-word *:m-0">{title}</Markdown>
                        {subtask?.status && (
                            <Badge variant={statusVariants[subtask.status] ?? 'secondary'}>{subtask.status}</Badge>
                        )}
                    </div>
                    <FlowAutomationSummary
                        className="text-muted-foreground text-xs"
                        metrics={group.metrics}
                    />
                </div>
            </div>
            {isOpen && (
                <div className="flex flex-col gap-3 p-3">
                    {subtask?.description && (
                        <Markdown
                            className="prose-xs prose-fixed text-muted-foreground wrap-break-word"
                            searchValue={searchValue}
                        >
                            {subtask.description}
                        </Markdown>
                    )}
                    {group.agentGroups.map((agentGroup) => (
                        <FlowAutomationAgentGroup
                            group={agentGroup}
                            key={agentGroup.agent}
                            searchValue={searchValue}
                        />
                    ))}
                    {group.looseLogs.map((log) => (
                        <FlowMessage
                            key={log.id}
                            log={log}
                            searchValue={searchValue}
                        />
                    ))}
                </div>
            )}
        </div>
    );
}

export default memo(FlowAutomationGroup);
