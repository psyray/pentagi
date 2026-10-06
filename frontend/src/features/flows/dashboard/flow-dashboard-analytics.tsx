import { useState } from 'react';

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
    FlowDashboardArtifacts,
    FlowDashboardAttackSurface,
    FlowDashboardCredentialsStatus,
    FlowDashboardDetectedCVEs,
    FlowDashboardInfrastructureMap,
    FlowDashboardOpenPorts,
    FlowDashboardToolUsage,
    FlowDashboardVulnerabilityBreakdown,
} from '@/features/flows/dashboard/flow-dashboard-blocks';
import { FlowDashboardTagStats } from '@/features/flows/dashboard/flow-dashboard-tag-stats';
import { FlowGraphView } from '@/features/flows/dashboard/flow-graph-view';
import { AttackGraphView } from '@/graphql/types';

// FlowDashboardAnalytics is the Graphiti-backed Analytics tab. It offers four
// purpose-built graph views (the EE "Main Attack Chain", "Action Chain",
// "Infrastructure" and "Priv Esc Chain"). The textual metric blocks live under
// the Main Attack Chain graph, on the same scrolling page, as in the EE.
function FlowDashboardAnalytics({ flowId }: { flowId: string }) {
    const [tab, setTab] = useState('main');

    return (
        <Tabs
            className="flex size-full flex-col"
            onValueChange={setTab}
            value={tab}
        >
            <TabsList className="flex w-fit">
                <TabsTrigger value="main">Main Attack Chain</TabsTrigger>
                <TabsTrigger value="action">Action Chain</TabsTrigger>
                <TabsTrigger value="infra">Infrastructure</TabsTrigger>
                <TabsTrigger value="privesc">Priv Esc Chain</TabsTrigger>
            </TabsList>

            <TabsContent
                className="mt-3 flex-1 overflow-auto pr-1"
                value="main"
            >
                <div className="flex flex-col gap-4">
                    <FlowGraphView
                        flowId={flowId}
                        view={AttackGraphView.Main}
                    />
                    <FlowDashboardTagStats flowId={flowId} />
                    <FlowDashboardAttackSurface flowId={flowId} />
                    <FlowDashboardCredentialsStatus flowId={flowId} />
                    <FlowDashboardInfrastructureMap flowId={flowId} />
                    <FlowDashboardOpenPorts flowId={flowId} />
                    <FlowDashboardVulnerabilityBreakdown flowId={flowId} />
                    <FlowDashboardDetectedCVEs flowId={flowId} />
                    <FlowDashboardArtifacts flowId={flowId} />
                    <FlowDashboardToolUsage flowId={flowId} />
                </div>
            </TabsContent>

            <TabsContent
                className="mt-3 flex-1 overflow-auto pr-1"
                value="action"
            >
                <FlowGraphView
                    allowFullToggle
                    flowId={flowId}
                    view={AttackGraphView.Action}
                />
            </TabsContent>

            <TabsContent
                className="mt-3 flex-1 overflow-auto pr-1"
                value="infra"
            >
                <FlowGraphView
                    flowId={flowId}
                    view={AttackGraphView.Infra}
                />
            </TabsContent>

            <TabsContent
                className="mt-3 flex-1 overflow-auto pr-1"
                value="privesc"
            >
                <FlowGraphView
                    flowId={flowId}
                    view={AttackGraphView.Privesc}
                />
            </TabsContent>
        </Tabs>
    );
}

export default FlowDashboardAnalytics;
