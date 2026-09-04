import { useQuery } from '@apollo/client/react';
import { useState } from 'react';

import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { FlowDashboardAttackGraph } from '@/features/flows/dashboard/flow-dashboard-attack-graph';
import { FlowDashboardArtifacts, FlowDashboardAttackSurface, FlowDashboardCredentialsStatus, FlowDashboardDetectedCVEs, FlowDashboardInfrastructureMap, FlowDashboardOpenPorts, FlowDashboardToolUsage, FlowDashboardValidAccesses, FlowDashboardVulnerabilityBreakdown } from '@/features/flows/dashboard/flow-dashboard-blocks';
import { FlowDashboardOverview } from '@/features/flows/dashboard/flow-dashboard-overview';
import { FlowDashboardTagStats } from '@/features/flows/dashboard/flow-dashboard-tag-stats';
import { KgDashboardEnabledDocument } from '@/graphql/types';
import { useFlow } from '@/providers/flow-provider';

function FlowDashboard() {
    const { flowId } = useFlow();

    // Detect whether the Graphiti / Neo4j dashboard is enabled on the backend.
    // When it is off, the Graphiti sub-tabs are hidden and only the LLM Overview
    // remains. The query is cheap (a boolean) and is not polled: enabling
    // Graphiti requires a backend restart, so a one-shot read is enough.
    const { data: kgData } = useQuery(KgDashboardEnabledDocument, {
        fetchPolicy: 'cache-first',
    });
    const kgEnabled = kgData?.kgDashboardEnabled ?? false;

    const [tab, setTab] = useState('overview');

    // When Graphiti is disabled, only the Overview tab exists. The Graphiti
    // sub-tabs are not rendered (see the kgEnabled guard below), so the active
    // tab is clamped to 'overview' here rather than via an effect, to avoid a
    // setState-in-effect cascade if the flag flips while a Graphiti tab is open.
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
                {kgEnabled ? (
                    <>
                        <TabsTrigger value="attackchain">Attack chain</TabsTrigger>
                        <TabsTrigger value="surface">Attack surface</TabsTrigger>
                        <TabsTrigger value="credentials">Credentials</TabsTrigger>
                        <TabsTrigger value="access">Valid access</TabsTrigger>
                        <TabsTrigger value="infra">Infrastructure</TabsTrigger>
                        <TabsTrigger value="vulns">Vulnerabilities</TabsTrigger>
                        <TabsTrigger value="tools">Tools &amp; artifacts</TabsTrigger>
                    </>
                ) : null}
            </TabsList>

            <TabsContent className="mt-3 flex-1 overflow-auto" value="overview">
                <FlowDashboardOverview flowId={flowId} />
            </TabsContent>

            {kgEnabled ? (
                <>
                    <TabsContent className="mt-3 flex-1 overflow-auto" value="attackchain">
                        <div className="flex flex-col gap-4 pr-0">
                            <FlowDashboardAttackGraph flowId={flowId} />
                            <FlowDashboardTagStats flowId={flowId} />
                        </div>
                    </TabsContent>
                    <TabsContent className="mt-3 flex-1 overflow-auto pr-1" value="surface">
                        <FlowDashboardAttackSurface flowId={flowId} />
                    </TabsContent>
                    <TabsContent className="mt-3 flex-1 overflow-auto pr-1" value="credentials">
                        <FlowDashboardCredentialsStatus flowId={flowId} />
                    </TabsContent>
                    <TabsContent className="mt-3 flex-1 overflow-auto pr-1" value="access">
                        <FlowDashboardValidAccesses flowId={flowId} />
                    </TabsContent>
                    <TabsContent className="mt-3 flex-1 overflow-auto pr-1" value="infra">
                        <div className="flex flex-col gap-4">
                            <FlowDashboardInfrastructureMap flowId={flowId} />
                            <FlowDashboardOpenPorts flowId={flowId} />
                        </div>
                    </TabsContent>
                    <TabsContent className="mt-3 flex-1 overflow-auto pr-1" value="vulns">
                        <div className="flex flex-col gap-4">
                            <FlowDashboardVulnerabilityBreakdown flowId={flowId} />
                            <FlowDashboardDetectedCVEs flowId={flowId} />
                        </div>
                    </TabsContent>
                    <TabsContent className="mt-3 flex-1 overflow-auto pr-1" value="tools">
                        <div className="flex flex-col gap-4">
                            <FlowDashboardToolUsage flowId={flowId} />
                            <FlowDashboardArtifacts flowId={flowId} />
                        </div>
                    </TabsContent>
                </>
            ) : null}
        </Tabs>
    );
}

export default FlowDashboard;