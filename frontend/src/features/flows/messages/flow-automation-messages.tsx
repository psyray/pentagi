import { zodResolver } from '@hookform/resolvers/zod';
import { ChevronDown, ChevronsDownUp, ChevronsUpDown, Inbox, ListFilter, Search, X } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useDebouncedCallback } from 'use-debounce';
import { z } from 'zod';

import type { MessageLogContentFragmentFragment } from '@/graphql/types';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Form, FormControl, FormField } from '@/components/ui/form';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group';
import { StatusType } from '@/graphql/types';
import { useAutoScroll } from '@/hooks/use-auto-scroll';
import { cn } from '@/lib/utils';
import { useFlow } from '@/providers/flow-provider';

import { FlowForm, type FlowFormValues } from '../flow-form';
import FlowTasksDropdown from '../flow-tasks-dropdown';
import FlowAutomationGroup from './flow-automation-group';
import { buildAutomationItems } from './flow-automation-groups';
import {
    agentStateKey,
    type AutomationAgentState,
    type AutomationViewState,
    emptyAutomationViewState,
    loadAutomationViewState,
    saveAutomationViewState,
} from './flow-automation-view-state';
import FlowMessage from './flow-message';

interface FlowAutomationMessagesProps {
    className?: string;
}

const searchFormSchema = z.object({
    filter: z
        .object({
            subtaskIds: z.array(z.string()),
            taskIds: z.array(z.string()),
        })
        .optional(),
    search: z.string(),
});

function FlowAutomationMessages({ className }: FlowAutomationMessagesProps) {
    const { flowData, flowId, flowStatus, stopAutomation, submitAutomationMessage } = useFlow();

    const logs = useMemo(() => flowData?.messageLogs ?? [], [flowData?.messageLogs]);

    const [debouncedSearchValue, setDebouncedSearchValue] = useState('');
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [isCanceling, setIsCanceling] = useState(false);

    const { containerRef, endRef, hasNewMessages, isScrolledToBottom, scrollToEnd } = useAutoScroll(logs, flowId);

    // Client-side view state, restored per flow: which subtasks are expanded, which agent runs
    // are open/watched, and the last expanded subtask used to position the reader on return.
    const [viewState, setViewState] = useState<AutomationViewState>(() =>
        flowId ? loadAutomationViewState(flowId) : emptyAutomationViewState,
    );
    const [loadedFlowId, setLoadedFlowId] = useState(flowId);
    const positionedFlowIdRef = useRef<null | string>(null);

    if (flowId !== loadedFlowId) {
        setLoadedFlowId(flowId);
        setViewState(flowId ? loadAutomationViewState(flowId) : emptyAutomationViewState);
        positionedFlowIdRef.current = null;
    }

    useEffect(() => {
        if (flowId) {
            saveAutomationViewState(flowId, viewState);
        }
    }, [flowId, viewState]);

    const form = useForm<z.infer<typeof searchFormSchema>>({
        defaultValues: {
            filter: {
                subtaskIds: [],
                taskIds: [],
            },
            search: '',
        },
        resolver: zodResolver(searchFormSchema),
    });

    const searchValue = form.watch('search');
    const filter = form.watch('filter');

    const debouncedUpdateSearch = useDebouncedCallback((value: string) => {
        setDebouncedSearchValue(value);
    }, 500);

    useEffect(() => {
        debouncedUpdateSearch(searchValue);

        return () => {
            debouncedUpdateSearch.cancel();
        };
    }, [searchValue, debouncedUpdateSearch]);

    useEffect(() => {
        return () => {
            debouncedUpdateSearch.cancel();
        };
    }, [debouncedUpdateSearch]);

    useEffect(() => {
        form.reset({
            filter: {
                subtaskIds: [],
                taskIds: [],
            },
            search: '',
        });
        setDebouncedSearchValue('');
        debouncedUpdateSearch.cancel();
    }, [flowId, form, debouncedUpdateSearch]);

    const hasActiveFilters = useMemo(() => {
        const hasSearch = !!searchValue.trim();
        const hasTaskFilters = !!(filter?.taskIds?.length || filter?.subtaskIds?.length);

        return hasSearch || hasTaskFilters;
    }, [searchValue, filter]);

    const filteredLogs = useMemo(() => {
        const search = debouncedSearchValue.toLowerCase().trim();

        let filtered = logs || [];

        if (search) {
            // Search is limited to the short message text: tool output and thinking of collapsed
            // subtasks are not loaded (lazy) and would not be searched anyway.
            filtered = filtered.filter((log) => log.message.toLowerCase().includes(search));
        }

        if (filter?.taskIds?.length || filter?.subtaskIds?.length) {
            const selectedTaskIds = new Set(filter.taskIds ?? []);
            const selectedSubtaskIds = new Set(filter.subtaskIds ?? []);

            filtered = filtered.filter((log) => {
                if (log.taskId && selectedTaskIds.has(log.taskId)) {
                    return true;
                }

                if (log.subtaskId && selectedSubtaskIds.has(log.subtaskId)) {
                    return true;
                }

                return false;
            });
        }

        return filtered;
    }, [logs, debouncedSearchValue, filter]);

    const subtasks = useMemo(() => (flowData?.tasks ?? []).flatMap((task) => task.subtasks ?? []), [flowData?.tasks]);

    const msgchains = useMemo(() => flowData?.msgchains ?? [], [flowData?.msgchains]);

    // Heavy content of the messages written outside a subtask, loaded with the flow (small set).
    const looseContentById = useMemo(() => {
        const byId = new Map<string, MessageLogContentFragmentFragment>();

        for (const log of flowData?.flowLooseMessageLogs ?? []) {
            byId.set(log.id, log);
        }

        return byId;
    }, [flowData?.flowLooseMessageLogs]);

    const items = useMemo(
        () => buildAutomationItems(filteredLogs, subtasks, msgchains),
        [filteredLogs, subtasks, msgchains],
    );

    const expandedSubtasks = useMemo(() => new Set(viewState.expandedSubtasks), [viewState.expandedSubtasks]);

    const renderedSubtaskIds = useMemo(
        () => items.flatMap((item) => (item.kind === 'subtask' ? [item.group.subtaskId] : [])),
        [items],
    );

    const collapsedCount = renderedSubtaskIds.filter((id) => !expandedSubtasks.has(id)).length;
    const isAllExpanded = renderedSubtaskIds.length > 0 && collapsedCount === 0;

    // Position the reader on the last subtask they had expanded, instead of the bottom of the list.
    useEffect(() => {
        if (!flowId || positionedFlowIdRef.current === flowId) {
            return;
        }

        const target = viewState.lastExpandedSubtaskId;

        positionedFlowIdRef.current = flowId;

        if (!target || !expandedSubtasks.has(target)) {
            return;
        }

        const frame = requestAnimationFrame(() => {
            document.querySelector(`[data-subtask-id="${CSS.escape(target)}"]`)?.scrollIntoView({ block: 'start' });
        });

        return () => cancelAnimationFrame(frame);
    }, [flowId, viewState.lastExpandedSubtaskId, expandedSubtasks, items]);

    const setSubtaskOpen = (subtaskId: string, open: boolean) => {
        setViewState((prev) => ({
            ...prev,
            expandedSubtasks: open
                ? Array.from(new Set([...prev.expandedSubtasks, subtaskId]))
                : prev.expandedSubtasks.filter((id) => id !== subtaskId),
            lastExpandedSubtaskId: open ? subtaskId : prev.lastExpandedSubtaskId,
        }));
    };

    const setAgentState = (subtaskId: string, agent: string, value: AutomationAgentState) => {
        setViewState((prev) => ({
            ...prev,
            agentStates: { ...prev.agentStates, [agentStateKey(subtaskId, agent)]: value },
        }));
    };

    const expandAll = () => {
        setViewState((prev) => ({ ...prev, expandedSubtasks: renderedSubtaskIds }));
    };

    const collapseAll = () => {
        setViewState((prev) => ({ ...prev, expandedSubtasks: [] }));
    };

    const placeholder = useMemo(() => {
        if (!flowId) {
            return 'Select a flow...';
        }

        switch (flowStatus) {
            case StatusType.Created: {
                return 'The flow is starting...';
            }

            case StatusType.Failed:
            case StatusType.Finished: {
                return 'This flow has ended. Create a new one to continue.';
            }

            case StatusType.Running: {
                return 'Steer the running task, or click Stop to interrupt...';
            }

            case StatusType.Waiting: {
                return 'Provide additional context or instructions...';
            }

            default: {
                return 'Type your message...';
            }
        }
    }, [flowId, flowStatus]);

    const handleSubmitMessage = async (values: FlowFormValues) => {
        setIsSubmitting(true);

        try {
            return await submitAutomationMessage(values);
        } finally {
            setIsSubmitting(false);
        }
    };

    const handleStopAutomation = async () => {
        setIsCanceling(true);

        try {
            return await stopAutomation();
        } finally {
            setIsCanceling(false);
        }
    };

    const handleResetFilters = () => {
        form.reset({
            filter: {
                subtaskIds: [],
                taskIds: [],
            },
            search: '',
        });
        setDebouncedSearchValue('');
        debouncedUpdateSearch.cancel();
    };

    const isFormDisabled = flowStatus === StatusType.Finished || flowStatus === StatusType.Failed;
    const isFormCreating = flowStatus === StatusType.Created;
    const isRunning = flowStatus === StatusType.Running;
    const isProviderChangeAllowed = flowStatus === StatusType.Waiting;

    return (
        <div className={cn('flex h-full flex-col', className)}>
            <div className="bg-background sticky top-0 z-10 pb-4">
                <Form {...form}>
                    <div className="flex gap-2 p-px">
                        <FormField
                            control={form.control}
                            name="search"
                            render={({ field }) => (
                                <FormControl>
                                    <InputGroup className="flex-1">
                                        <InputGroupAddon>
                                            <Search />
                                        </InputGroupAddon>
                                        <InputGroupInput
                                            {...field}
                                            autoComplete="off"
                                            placeholder="Search messages..."
                                            type="text"
                                        />
                                        {field.value && (
                                            <InputGroupAddon align="inline-end">
                                                <InputGroupButton
                                                    aria-label="Clear message search"
                                                    onClick={() => {
                                                        form.reset({ search: '' });
                                                        setDebouncedSearchValue('');
                                                        debouncedUpdateSearch.cancel();
                                                    }}
                                                    type="button"
                                                >
                                                    <X />
                                                </InputGroupButton>
                                            </InputGroupAddon>
                                        )}
                                    </InputGroup>
                                </FormControl>
                            )}
                        />
                        <FormField
                            control={form.control}
                            name="filter"
                            render={({ field }) => (
                                <FormControl>
                                    <FlowTasksDropdown
                                        onChange={field.onChange}
                                        value={field.value}
                                    />
                                </FormControl>
                            )}
                        />
                        <Button
                            aria-label={isAllExpanded ? 'Collapse all subtasks' : 'Expand all subtasks'}
                            disabled={renderedSubtaskIds.length === 0}
                            onClick={isAllExpanded ? collapseAll : expandAll}
                            size="icon"
                            type="button"
                            variant="outline"
                        >
                            {isAllExpanded ? <ChevronsDownUp /> : <ChevronsUpDown />}
                        </Button>
                    </div>
                </Form>
                {debouncedSearchValue.trim() && collapsedCount > 0 && (
                    <p className="text-muted-foreground mt-2 text-xs">
                        Search covers the message text only. {collapsedCount} collapsed subtask
                        {collapsedCount === 1 ? '' : 's'} {collapsedCount === 1 ? 'is' : 'are'} not searched in full,
                        expand them to include their details.
                    </p>
                )}
            </div>

            {filteredLogs.length > 0 ? (
                <div className="relative h-full overflow-y-hidden">
                    <div
                        className="flex h-full flex-col gap-4 overflow-y-auto"
                        ref={containerRef}
                    >
                        {items.map((item) => {
                            if (item.kind === 'subtask') {
                                return (
                                    <FlowAutomationGroup
                                        agentStates={viewState.agentStates}
                                        flowId={flowId ?? ''}
                                        group={item.group}
                                        isOpen={expandedSubtasks.has(item.group.subtaskId)}
                                        key={`subtask-${item.group.subtaskId}`}
                                        onAgentStateChange={(agent, value) =>
                                            setAgentState(item.group.subtaskId, agent, value)
                                        }
                                        onOpenChange={(open) => setSubtaskOpen(item.group.subtaskId, open)}
                                        searchValue={debouncedSearchValue}
                                    />
                                );
                            }

                            const content = looseContentById.get(item.log.id);

                            return (
                                <FlowMessage
                                    key={item.log.id}
                                    log={content ? { ...item.log, ...content } : item.log}
                                    searchValue={debouncedSearchValue}
                                />
                            );
                        })}
                        <div ref={endRef} />
                    </div>

                    {!isScrolledToBottom && (
                        <Button
                            aria-label="Scroll to latest message"
                            className="absolute right-4 bottom-4 z-10 shadow-md hover:shadow-lg"
                            onClick={() => scrollToEnd()}
                            size="icon-sm"
                            type="button"
                            variant="outline"
                        >
                            <ChevronDown />
                            {hasNewMessages && (
                                <span className="bg-primary absolute -top-1.5 -right-1.5 size-3 rounded-full" />
                            )}
                        </Button>
                    )}
                </div>
            ) : hasActiveFilters ? (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <ListFilter />
                        </EmptyMedia>
                        <EmptyTitle>No messages found</EmptyTitle>
                        <EmptyDescription>Try adjusting your search or filter parameters</EmptyDescription>
                    </EmptyHeader>
                    <EmptyContent>
                        <Button
                            onClick={handleResetFilters}
                            variant="outline"
                        >
                            <X />
                            Reset filters
                        </Button>
                    </EmptyContent>
                </Empty>
            ) : (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <Inbox />
                        </EmptyMedia>
                        <EmptyTitle>No active tasks</EmptyTitle>
                        <EmptyDescription>
                            Starting a new task may take some time as the PentAGI agent downloads the required Docker
                            image
                        </EmptyDescription>
                    </EmptyHeader>
                </Empty>
            )}

            <div className="bg-background sticky bottom-0 p-px">
                <FlowForm
                    defaultValues={{
                        providerName: flowData?.flow?.provider?.name ?? '',
                    }}
                    isCanceling={isCanceling}
                    isDisabled={isFormDisabled}
                    isLoading={isFormCreating}
                    isProviderDisabled={!isProviderChangeAllowed}
                    isRunning={isRunning}
                    isSubmitting={isSubmitting}
                    onCancel={handleStopAutomation}
                    onSubmit={handleSubmitMessage}
                    placeholder={placeholder}
                    type={'automation'}
                />
            </div>
        </div>
    );
}

export default FlowAutomationMessages;
