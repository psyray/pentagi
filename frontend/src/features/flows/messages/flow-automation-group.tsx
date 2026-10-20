import { useQuery } from '@apollo/client/react';
import { ChevronDown } from 'lucide-react';
import { memo, useMemo } from 'react';

import type { BadgeVariant } from '@/components/ui/badge';
import type { MessageLogContentFragmentFragment, StatusType } from '@/graphql/types';

import Markdown from '@/components/shared/markdown';
import { Badge } from '@/components/ui/badge';
import { Spinner } from '@/components/ui/spinner';
import { StatusType as StatusTypeEnum, SubtaskMessageLogsDocument } from '@/graphql/types';
import { cn } from '@/lib/utils';

import type { AgentGroup, SubtaskGroup } from './flow-automation-groups';
import type { AutomationAgentState } from './flow-automation-view-state';

import FlowAutomationAgentGroup from './flow-automation-agent-group';
import FlowAutomationSummary from './flow-automation-summary';
import { agentStateKey } from './flow-automation-view-state';
import FlowMessage from './flow-message';

const statusVariants: Record<StatusType, BadgeVariant> = {
    [StatusTypeEnum.Created]: 'blue',
    [StatusTypeEnum.Failed]: 'red',
    [StatusTypeEnum.Finished]: 'green',
    [StatusTypeEnum.Running]: 'purple',
    [StatusTypeEnum.Waiting]: 'yellow',
};

const closedAgentState: AutomationAgentState = { open: false, watched: false };

interface FlowAutomationGroupProps {
    agentStates: Record<string, AutomationAgentState>;
    flowId: string;
    group: SubtaskGroup;
    isOpen: boolean;
    onAgentStateChange: (agent: string, value: AutomationAgentState) => void;
    onOpenChange: (open: boolean) => void;
    searchValue?: string;
}

function FlowAutomationGroup({
    agentStates,
    flowId,
    group,
    isOpen,
    onAgentStateChange,
    onOpenChange,
    searchValue = '',
}: FlowAutomationGroupProps) {
    const { subtask } = group;
    const title = subtask?.title || `Subtask ${group.subtaskId}`;

    // Heavy content (tool output, thinking) is loaded only once the subtask is expanded, then merged
    // into each message by id.
    const { data: contentData, loading: isContentLoading } = useQuery(SubtaskMessageLogsDocument, {
        skip: !isOpen || !flowId,
        variables: { flowId, subtaskId: group.subtaskId },
    });

    const contentById = useMemo(() => {
        const byId = new Map<string, MessageLogContentFragmentFragment>();

        for (const log of contentData?.subtaskMessageLogs ?? []) {
            byId.set(log.id, log);
        }

        return byId;
    }, [contentData]);

    const agentGroups: AgentGroup[] = useMemo(
        () =>
            group.agentGroups.map((group_) => ({
                ...group_,
                logs: group_.logs.map((log) => {
                    const content = contentById.get(log.id);

                    return content ? { ...log, ...content } : log;
                }),
            })),
        [group.agentGroups, contentById],
    );

    const looseLogs = useMemo(
        () =>
            group.looseLogs.map((log) => {
                const content = contentById.get(log.id);

                return content ? { ...log, ...content } : log;
            }),
        [group.looseLogs, contentById],
    );

    const toggle = () => onOpenChange(!isOpen);

    return (
        <div
            className="bg-card text-card-foreground max-w-[90%] rounded-xl border shadow-sm"
            data-subtask-id={group.subtaskId}
        >
            <div
                aria-expanded={isOpen}
                className="flex w-full cursor-pointer items-start gap-2 p-3 text-left"
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
                        'text-muted-foreground mt-0.5 size-5 shrink-0 transition-transform',
                        !isOpen && '-rotate-90',
                    )}
                />
                <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <div className="flex items-center gap-2">
                        <Markdown className="prose-xs prose-fixed wrap-break-word *:m-0">{title}</Markdown>
                        {subtask?.status && (
                            <Badge
                                className="ml-auto shrink-0"
                                variant={statusVariants[subtask.status] ?? 'secondary'}
                            >
                                {subtask.status}
                            </Badge>
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
                    {isContentLoading && !contentData && (
                        <div className="text-muted-foreground flex items-center gap-2 text-xs">
                            <Spinner className="size-3" />
                            Loading subtask messages...
                        </div>
                    )}
                    {subtask?.description && (
                        <Markdown
                            className="prose-xs prose-fixed text-muted-foreground wrap-break-word"
                            searchValue={searchValue}
                        >
                            {subtask.description}
                        </Markdown>
                    )}
                    {agentGroups.map((agentGroup) => {
                        const key = agentStateKey(group.subtaskId, agentGroup.agent);
                        const state = agentStates[key] ?? closedAgentState;

                        return (
                            <FlowAutomationAgentGroup
                                group={agentGroup}
                                isOpen={state.open}
                                isWatched={state.watched}
                                key={agentGroup.agent}
                                onOpenChange={(open) => onAgentStateChange(agentGroup.agent, { ...state, open })}
                                onWatchedChange={(watched) =>
                                    onAgentStateChange(agentGroup.agent, { ...state, watched })
                                }
                                searchValue={searchValue}
                                subtaskTitle={subtask?.title}
                            />
                        );
                    })}
                    {looseLogs.map((log) => (
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
