import { describe, expect, it } from 'vitest';

import type { MessageLogFragmentFragment, MsgchainFragmentFragment, SubtaskFragmentFragment } from '@/graphql/types';

import { AgentType, MessageLogType, ResultFormat, StatusType } from '@/graphql/types';

import type { AutomationItem } from './flow-automation-groups';

import { buildAutomationItems } from './flow-automation-groups';

const createdAt = '2026-10-06T12:00:00Z';

const log = (overrides: Partial<MessageLogFragmentFragment> = {}): MessageLogFragmentFragment => ({
    agent: null,
    createdAt,
    flowId: '1',
    id: '1',
    message: '',
    result: '',
    resultFormat: ResultFormat.Plain,
    subtaskId: null,
    taskId: null,
    thinking: null,
    type: MessageLogType.Answer,
    ...overrides,
});

const subtask = (overrides: Partial<SubtaskFragmentFragment> = {}): SubtaskFragmentFragment => ({
    budgetSeconds: null,
    createdAt,
    description: '',
    evidence: null,
    id: '9',
    result: '',
    status: StatusType.Running,
    taskId: '5',
    title: 'Recon',
    type: null,
    updatedAt: createdAt,
    ...overrides,
});

const chain = (overrides: Partial<MsgchainFragmentFragment> = {}): MsgchainFragmentFragment => ({
    createdAt,
    durationSeconds: 0,
    flowId: '1',
    id: '1',
    model: 'kimi-k3',
    modelProvider: 'openai',
    subtaskId: null,
    taskId: null,
    type: AgentType.Pentester,
    usageCacheIn: 0,
    usageCacheOut: 0,
    usageIn: 0,
    usageOut: 0,
    ...overrides,
});

const firstSubtask = (items: AutomationItem[]) => {
    const [first] = items;

    if (first?.kind !== 'subtask') {
        throw new Error('expected a subtask item');
    }

    return first;
};

describe('buildAutomationItems', () => {
    it('groups messages by subtask and then by agent, keeping the subtask order', () => {
        const logs = [
            log({ agent: AgentType.PrimaryAgent, id: '1', subtaskId: '9' }),
            log({ agent: AgentType.Pentester, id: '2', subtaskId: '9' }),
            log({ agent: AgentType.PrimaryAgent, id: '3', subtaskId: '10' }),
        ];
        const subtasks = [
            subtask({ createdAt: '2026-10-06T12:00:00.000Z', id: '9', updatedAt: '2026-10-06T12:00:56.700Z' }),
            subtask({ id: '10', title: 'Exploit' }),
        ];
        const msgchains = [
            chain({ durationSeconds: 4.2, subtaskId: '9', type: AgentType.PrimaryAgent, usageIn: 18000 }),
            chain({ durationSeconds: 30.7, subtaskId: '9', type: AgentType.Pentester, usageIn: 19000 }),
            chain({ durationSeconds: 2, subtaskId: '10', type: AgentType.PrimaryAgent, usageIn: 1000 }),
        ];

        const items = buildAutomationItems(logs, subtasks, msgchains);

        expect(items.map((item) => item.kind)).toEqual(['subtask', 'subtask']);

        const first = firstSubtask(items);

        expect(first.group.subtaskId).toBe('9');
        expect(first.group.subtask?.title).toBe('Recon');
        expect(first.group.agentGroups.map((group) => group.agent)).toEqual([
            AgentType.PrimaryAgent,
            AgentType.Pentester,
        ]);
        expect(first.group.agentGroups[0]?.metrics).toEqual({ durationSeconds: 4.2, messages: 1, tokens: 18000 });
        expect(first.group.agentGroups[1]?.metrics).toEqual({ durationSeconds: 30.7, messages: 1, tokens: 19000 });
        // The subtask keeps its wall-clock span (56.7s), not the sum of its agents (34.9s).
        expect(first.group.metrics).toEqual({ durationSeconds: 56.7, messages: 2, tokens: 37000 });
    });

    it('emits messages written outside a subtask as standalone items', () => {
        const logs = [log({ id: '1' }), log({ agent: AgentType.Pentester, id: '2', subtaskId: '9' }), log({ id: '3' })];

        const items = buildAutomationItems(logs, [subtask({ id: '9' })], []);

        expect(items.map((item) => item.kind)).toEqual(['message', 'subtask', 'message']);
    });

    it('keeps messages without an agent flat under their subtask', () => {
        const logs = [
            log({ agent: AgentType.Pentester, id: '1', subtaskId: '9' }),
            log({ agent: null, id: '2', subtaskId: '9' }),
        ];

        const first = firstSubtask(buildAutomationItems(logs, [subtask({ id: '9' })], []));

        expect(first.group.agentGroups).toHaveLength(1);
        expect(first.group.looseLogs.map((loose) => loose.id)).toEqual(['2']);
        expect(first.group.metrics.messages).toBe(2);
    });

    it('falls back to a synthetic title when the subtask is unknown', () => {
        const first = firstSubtask(
            buildAutomationItems([log({ agent: AgentType.Pentester, id: '1', subtaskId: '99' })], [], []),
        );

        expect(first.group.subtask).toBeUndefined();
        expect(first.group.subtaskId).toBe('99');
    });

    it('falls back to the summed agent durations when the subtask is unknown', () => {
        const first = firstSubtask(
            buildAutomationItems(
                [log({ agent: AgentType.Pentester, id: '1', subtaskId: '99' })],
                [],
                [chain({ durationSeconds: 12.5, subtaskId: '99', type: AgentType.Pentester, usageIn: 100 })],
            ),
        );

        expect(first.group.metrics.durationSeconds).toBe(12.5);
        expect(first.group.metrics.tokens).toBe(100);
    });
});
