import { useQuery } from '@apollo/client/react';
import { useState } from 'react';

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import FlowDashboardAnalytics from '@/features/flows/dashboard/flow-dashboard-analytics';
import { FlowDashboardOverview } from '@/features/flows/dashboard/flow-dashboard-overview';
import { KgDashboardEnabledDocument } from '@/graphql/types';
import { useFlow } from '@/providers/flow-provider';

function FlowDashboard() {
    const { flowId } = useFlow();

    // Detect whether the Graphiti / Neo4j dashboard is enabled on the backend.
    // When it is off, the Analytics tab is hidden and only the LLM Overview
    // remains. The query is cheap (a boolean) and is not polled: enabling
    // Graphiti requires a backend restart, so a one-shot read is enough.
    const { data: kgData } = useQuery(KgDashboardEnabledDocument, {
        fetchPolicy: 'cache-first',
    });
    const kgEnabled = kgData?.kgDashboardEnabled ?? false;

    const [tab, setTab] = useState('overview');

    // When Graphiti is disabled, only the Overview tab exists. The Analytics
    // tab is not rendered (see the kgEnabled guard below), so the active tab is
    // clamped to 'overview' here rather than via an effect, to avoid a
    // setState-in-effect cascade if the flag flips while Analytics is open.
    const effectiveTab = kgEnabled ? tab : 'overview';

    if (!flowId) {
        return (
            <div className="text-muted-foreground flex items-center justify-center py-12">
                Select a flow to view the dashboard
            </div>
        );
    }

    return (
        <Tabs
            className="flex size-full flex-col"
            onValueChange={setTab}
            value={effectiveTab}
        >
            <TabsList className="flex w-fit">
                <TabsTrigger value="overview">Overview</TabsTrigger>
                {kgEnabled ? <TabsTrigger value="analytics">Analytics</TabsTrigger> : null}
            </TabsList>

            <TabsContent
                className="mt-3 flex-1 overflow-auto"
                value="overview"
            >
                <FlowDashboardOverview flowId={flowId} />
            </TabsContent>

            {kgEnabled ? (
                <TabsContent
                    className="mt-3 flex min-h-0 flex-1 flex-col overflow-hidden"
                    value="analytics"
                >
                    <FlowDashboardAnalytics flowId={flowId} />
                </TabsContent>
            ) : null}
        </Tabs>
    );
}

export default FlowDashboard;
