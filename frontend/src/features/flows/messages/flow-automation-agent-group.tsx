import { ChevronDown } from 'lucide-react';
import { memo, useState } from 'react';

import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

import type { AgentGroup } from './flow-automation-groups';

import FlowAgentIcon from '../agents/flow-agent-icon';
import { agentGroupLabel, isPlannerAgent } from './flow-automation-groups';
import FlowAutomationSummary from './flow-automation-summary';
import FlowMessage from './flow-message';

interface FlowAutomationAgentGroupProps {
    group: AgentGroup;
    searchValue?: string;
}

function FlowAutomationAgentGroup({ group, searchValue = '' }: FlowAutomationAgentGroupProps) {
    const [isOpen, setIsOpen] = useState(true);
    const isPlanner = isPlannerAgent(group.agent);

    return (
        <div className="bg-card/40 rounded-lg border">
            <button
                aria-expanded={isOpen}
                className="flex w-full items-center gap-2 p-3 text-left"
                onClick={() => setIsOpen(!isOpen)}
                type="button"
            >
                <ChevronDown
                    className={cn(
                        'text-muted-foreground size-4 shrink-0 transition-transform',
                        !isOpen && '-rotate-90',
                    )}
                />
                <FlowAgentIcon
                    className="text-muted-foreground"
                    type={group.agent}
                />
                <span className="text-sm font-medium">{agentGroupLabel(group.agent)}</span>
                {!isPlanner && <Badge variant="outline">{group.agent}</Badge>}
                <FlowAutomationSummary
                    className="text-muted-foreground ml-auto text-xs"
                    metrics={group.metrics}
                />
            </button>
            {isOpen && (
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
