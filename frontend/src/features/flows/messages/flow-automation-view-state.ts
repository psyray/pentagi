import { z } from 'zod';

import { getStorageItem, setStorageItem } from '@/lib/local-storage';

const agentStateSchema = z.object({
    open: z.boolean(),
    watched: z.boolean(),
});

export const automationViewStateSchema = z.object({
    agentStates: z.record(z.string(), agentStateSchema).default({}),
    expandedSubtasks: z.array(z.string()).default([]),
    lastExpandedSubtaskId: z.string().nullish(),
});

export type AutomationAgentState = z.infer<typeof agentStateSchema>;
export type AutomationViewState = z.infer<typeof automationViewStateSchema>;

export const emptyAutomationViewState: AutomationViewState = {
    agentStates: {},
    expandedSubtasks: [],
    lastExpandedSubtaskId: null,
};

export const automationViewStateKey = (flowId: string): string => `pentagi:automation:view:${flowId}`;

export const loadAutomationViewState = (flowId: string): AutomationViewState =>
    getStorageItem(automationViewStateKey(flowId), automationViewStateSchema) ?? emptyAutomationViewState;

export const saveAutomationViewState = (flowId: string, state: AutomationViewState): void => {
    setStorageItem(automationViewStateKey(flowId), state);
};

/** Stable key for one agent run, scoped to its subtask, inside the flow's view state. */
export const agentStateKey = (subtaskId: string, agent: string): string => `${subtaskId}|${agent}`;
