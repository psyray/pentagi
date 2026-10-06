import { Maximize2 } from 'lucide-react';
import { useState } from 'react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog';

import type { AgentGroup } from './flow-automation-groups';

import FlowAgentIcon from '../agents/flow-agent-icon';
import { agentGroupLabel } from './flow-automation-groups';
import FlowAutomationSummary from './flow-automation-summary';
import FlowMessage from './flow-message';

interface FlowAutomationAgentPanelProps {
    group: AgentGroup;
    searchValue?: string;
    subtaskTitle?: string;
}

function FlowAutomationAgentPanel({ group, searchValue = '', subtaskTitle }: FlowAutomationAgentPanelProps) {
    const [isOpen, setIsOpen] = useState(false);
    const label = agentGroupLabel(group.agent);

    return (
        <Dialog
            onOpenChange={setIsOpen}
            open={isOpen}
        >
            <DialogTrigger asChild>
                <Button
                    aria-label={`Expand ${label}`}
                    size="icon-sm"
                    type="button"
                    variant="ghost"
                >
                    <Maximize2 />
                </Button>
            </DialogTrigger>
            <DialogContent className="flex h-[calc(100vh-2rem)] w-[calc(100vw-2rem)] max-w-[calc(100vw-2rem)] flex-col gap-0 p-0 sm:max-w-[calc(100vw-2rem)]">
                <DialogHeader className="shrink-0 border-b p-4">
                    <DialogTitle className="flex items-center gap-2">
                        <FlowAgentIcon type={group.agent} />
                        {label}
                        <Badge variant="outline">{group.agent}</Badge>
                    </DialogTitle>
                    <DialogDescription className="flex flex-wrap items-center gap-x-3 gap-y-1">
                        {subtaskTitle && <span className="truncate">{subtaskTitle}</span>}
                        <FlowAutomationSummary metrics={group.metrics} />
                    </DialogDescription>
                </DialogHeader>
                <div className="flex flex-1 flex-col gap-3 overflow-y-auto p-4">
                    {group.logs.map((log) => (
                        <FlowMessage
                            key={log.id}
                            log={log}
                            searchValue={searchValue}
                        />
                    ))}
                </div>
            </DialogContent>
        </Dialog>
    );
}

export default FlowAutomationAgentPanel;
