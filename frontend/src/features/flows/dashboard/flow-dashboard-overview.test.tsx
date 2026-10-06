import { render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

const query = vi.hoisted(() => ({ state: { error: undefined as Error | undefined, loading: false } }));

const stats = {
    totalUsageCacheIn: 0,
    totalUsageCacheOut: 0,
    totalUsageCostIn: 1,
    totalUsageCostOut: 0.5,
    totalUsageIn: 1000,
    totalUsageOut: 500,
};

vi.mock('@apollo/client/react', () => ({
    useQuery: () => ({
        data: {
            flowStatsByFlow: { totalAssistantsCount: 3, totalSubtasksCount: 2, totalTasksCount: 42 },
            toolcallsStatsByFlow: { totalCount: 7, totalDurationSeconds: 60 },
            toolcallsStatsByFunctionForFlow: [
                {
                    avgDurationSeconds: 4,
                    functionName: 'pentester',
                    isAgent: true,
                    isSubAgent: true,
                    totalCount: 4,
                    totalDurationSeconds: 12,
                },
                {
                    avgDurationSeconds: 1,
                    functionName: 'terminal',
                    isAgent: false,
                    isSubAgent: false,
                    totalCount: 9,
                    totalDurationSeconds: 9,
                },
            ],
            usageStatsByAgentTypeForFlow: [{ agentType: 'pentester', stats }],
            usageStatsByFlow: stats,
            usageStatsByModelAgentsForFlow: [],
        },
        ...query.state,
    }),
}));

import { FlowDashboardOverview } from './flow-dashboard-overview';

const placeholders = () => document.querySelectorAll('[data-slot="skeleton"], .animate-spin').length;

describe('FlowDashboardOverview', () => {
    it('keeps the figures and tables on screen while they refetch', () => {
        query.state = { error: undefined, loading: true };

        render(<FlowDashboardOverview flowId="5" />);

        expect(placeholders()).toBe(0);
        expect(screen.getByText('42')).toBeInTheDocument();
        // 'pentester' now appears both as an agent-type row and as a tool row.
        expect(screen.getAllByText('pentester').length).toBeGreaterThan(0);
    });

    it('counts only delegated sub-agent runs, not every tool call', () => {
        query.state = { error: undefined, loading: false };

        render(<FlowDashboardOverview flowId="5" />);

        // 4 pentester delegations; the 9 terminal calls are not sub-agent runs.
        const card = screen.getByText('Sub-agent runs').closest('[data-slot="card"]');

        expect(card).not.toBeNull();
        expect(within(card as HTMLElement).getByText('4')).toBeInTheDocument();
    });

    it('keeps them on screen when a refetch fails', () => {
        query.state = { error: new Error('stats unavailable'), loading: false };

        render(<FlowDashboardOverview flowId="5" />);

        expect(screen.queryByText("Couldn't load")).not.toBeInTheDocument();
        expect(screen.getByText('42')).toBeInTheDocument();
    });
});
