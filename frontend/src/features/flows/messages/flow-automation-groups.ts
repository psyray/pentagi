import type {
    AgentType,
    MessageLogFragmentFragment,
    MsgchainFragmentFragment,
    SubtaskFragmentFragment,
} from '@/graphql/types';

import { AgentType as AgentTypeEnum } from '@/graphql/types';

export const plannerAgent: AgentType = AgentTypeEnum.PrimaryAgent;
export const plannerLabel = 'Planner';
export const workerLabel = 'Working agent';

export interface AgentGroup {
    agent: AgentType;
    logs: MessageLogFragmentFragment[];
    metrics: GroupMetrics;
}

export type AutomationItem =
    | { group: SubtaskGroup; kind: 'subtask' }
    | { kind: 'message'; log: MessageLogFragmentFragment };

export interface GroupMetrics {
    durationSeconds: number;
    messages: number;
    tokens: number;
}

export interface SubtaskGroup {
    agentGroups: AgentGroup[];
    looseLogs: MessageLogFragmentFragment[];
    metrics: GroupMetrics;
    subtask: SubtaskFragmentFragment | undefined;
    subtaskId: string;
}

interface Usage {
    durationSeconds: number;
    tokens: number;
}

export function agentGroupLabel(agent: AgentType): string {
    return agent === plannerAgent ? plannerLabel : workerLabel;
}

/**
 * Groups messages by subtask and then by the agent that produced them, keeping the
 * chronological order of the top-level items. Messages written outside an agent run
 * (flow or task level, no subtask) are emitted as standalone items, and messages
 * without an agent stay flat under their subtask.
 */
export function buildAutomationItems(
    logs: MessageLogFragmentFragment[],
    subtasks: SubtaskFragmentFragment[],
    msgchains: MsgchainFragmentFragment[],
): AutomationItem[] {
    const subtaskById = new Map(subtasks.map((subtask) => [subtask.id, subtask]));
    const { perAgent, totals } = buildUsageIndexes(msgchains);

    const logsBySubtask = new Map<string, MessageLogFragmentFragment[]>();

    for (const log of logs) {
        if (!log.subtaskId) {
            continue;
        }

        const bucket = logsBySubtask.get(log.subtaskId) ?? [];
        bucket.push(log);
        logsBySubtask.set(log.subtaskId, bucket);
    }

    const groups = new Map<string, SubtaskGroup>();

    for (const [subtaskId, subtaskLogs] of logsBySubtask) {
        groups.set(subtaskId, buildSubtaskGroup(subtaskId, subtaskLogs, subtaskById.get(subtaskId), perAgent, totals));
    }

    const items: AutomationItem[] = [];
    const emitted = new Set<string>();

    for (const log of logs) {
        if (!log.subtaskId) {
            items.push({ kind: 'message', log });
            continue;
        }

        if (emitted.has(log.subtaskId)) {
            continue;
        }

        emitted.add(log.subtaskId);

        const group = groups.get(log.subtaskId);

        if (group) {
            items.push({ group, kind: 'subtask' });
        }
    }

    return items;
}

export function isPlannerAgent(agent: AgentType): boolean {
    return agent === plannerAgent;
}

function addChain(target: Usage, chain: MsgchainFragmentFragment): void {
    target.durationSeconds += chain.durationSeconds;
    target.tokens += chain.usageIn + chain.usageOut;
}

function buildSubtaskGroup(
    subtaskId: string,
    logs: MessageLogFragmentFragment[],
    subtask: SubtaskFragmentFragment | undefined,
    perAgent: Map<string, Usage>,
    totals: Map<string, Usage>,
): SubtaskGroup {
    const byAgent = new Map<AgentType, MessageLogFragmentFragment[]>();
    const looseLogs: MessageLogFragmentFragment[] = [];

    for (const log of logs) {
        if (!log.agent) {
            looseLogs.push(log);
            continue;
        }

        const bucket = byAgent.get(log.agent) ?? [];
        bucket.push(log);
        byAgent.set(log.agent, bucket);
    }

    const agentGroups: AgentGroup[] = [];

    for (const [agent, agentLogs] of byAgent) {
        const usage = perAgent.get(`${subtaskId}:${agent}`);

        agentGroups.push({
            agent,
            logs: agentLogs,
            metrics: {
                durationSeconds: usage?.durationSeconds ?? 0,
                messages: agentLogs.length,
                tokens: usage?.tokens ?? 0,
            },
        });
    }

    const total = totals.get(subtaskId);

    return {
        agentGroups,
        looseLogs,
        metrics: {
            durationSeconds: total?.durationSeconds ?? 0,
            messages: logs.length,
            tokens: total?.tokens ?? 0,
        },
        subtask,
        subtaskId,
    };
}

function buildUsageIndexes(msgchains: MsgchainFragmentFragment[]) {
    const totals = new Map<string, Usage>();
    const perAgent = new Map<string, Usage>();

    for (const chain of msgchains) {
        if (!chain.subtaskId) {
            continue;
        }

        const total = totals.get(chain.subtaskId) ?? { durationSeconds: 0, tokens: 0 };
        addChain(total, chain);
        totals.set(chain.subtaskId, total);

        const key = `${chain.subtaskId}:${chain.type}`;
        const agent = perAgent.get(key) ?? { durationSeconds: 0, tokens: 0 };
        addChain(agent, chain);
        perAgent.set(key, agent);
    }

    return { perAgent, totals };
}
