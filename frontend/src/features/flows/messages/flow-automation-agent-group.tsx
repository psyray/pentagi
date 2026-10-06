import { ChevronDown } from 'lucide-react';
import { memo } from 'react';

import { Badge } from '@/components/ui/badge';
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';

import type { AgentGroup } from './flow-automation-groups';

import FlowAgentIcon from '../agents/flow-agent-icon';
import FlowAutomationAgentPanel from './flow-automation-agent-panel';
import { agentGroupLabel, isPlannerAgent } from './flow-automation-groups';
import FlowAutomationSummary from './flow-automation-summary';
import FlowMessage from './flow-message';

interface FlowAutomationAgentGroupProps {
    group: AgentGroup;
    isOpen: boolean;
    isWatched: boolean;
    onOpenChange: (open: boolean) => void;
    onWatchedChange: (watched: boolean) => void;
    searchValue?: string;
    subtaskTitle?: string;
}

function FlowAutomationAgentGroup({
    group,
    isOpen,
    isWatched,
    onOpenChange,
    onWatchedChange,
    searchValue = '',
    subtaskTitle,
}: FlowAutomationAgentGroupProps) {
    const isPlanner = isPlannerAgent(group.agent);
    const isExpanded = isWatched || isOpen;
    const label = agentGroupLabel(group.agent);

    const toggle = () => onOpenChange(!isOpen);

    return (
        <div className={cn('bg-card/40 rounded-lg border', isWatched && 'ring-primary/40 ring-1')}>
            <div className="flex items-center gap-2 p-3">
                <div
                    aria-expanded={isExpanded}
                    className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 text-left"
                    onClick={toggle}
                    onKeyDown={(event) => {
                        if (event.key === 'Enter' || event.key === ' ') {
                            event.preventDefault();
                            toggle();
                        }
                    }}
                    role="button"
                    tabIndex={0}
                >
                    <ChevronDown
                        className={cn(
                            'text-muted-foreground size-4 shrink-0 transition-transform',
                            !isExpanded && '-rotate-90',
                        )}
                    />
                    <FlowAgentIcon
                        className="text-muted-foreground size-5"
                        type={group.agent}
                    />
                    <span className="text-sm font-medium">{label}</span>
                    {!isPlanner && <Badge variant="outline">{group.agent}</Badge>}
                    <FlowAutomationSummary
                        className="text-muted-foreground ml-auto shrink-0 text-xs"
                        metrics={group.metrics}
                    />
                </div>
                <Switch
                    aria-label={`Watch ${label}`}
                    checked={isWatched}
                    className="shrink-0"
                    onCheckedChange={onWatchedChange}
                    title="Watch this agent: keep its messages open"
                />
                <FlowAutomationAgentPanel
                    group={group}
                    searchValue={searchValue}
                    subtaskTitle={subtaskTitle}
                />
            </div>
            {isExpanded && (
                <div className="flex flex-col gap-3 px-3 pb-3">
                    {group.logs.map((log) => (
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

export default memo(FlowAutomationAgentGroup);
