import { useEffect, useRef } from 'react';

import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
import { Spinner } from '@/components/ui/spinner';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import FlowAgents from '@/features/flows/agents/flow-agents';
import FlowDashboard from '@/features/flows/dashboard/flow-dashboard';
import FlowFiles from '@/features/flows/files/flow-files';
import FlowAssistantMessages from '@/features/flows/messages/flow-assistant-messages';
import FlowAutomationMessages from '@/features/flows/messages/flow-automation-messages';
import FlowReports from '@/features/flows/reports/flow-reports';
import FlowScreenshots from '@/features/flows/screenshots/flow-screenshots';
import FlowShell from '@/features/flows/shell/flow-shell';
import FlowTasks from '@/features/flows/tasks/flow-tasks';
import FlowTerminal from '@/features/flows/terminal/flow-terminal';
import FlowTools from '@/features/flows/tools/flow-tools';
import FlowVectorStores from '@/features/flows/vector-stores/flow-vector-stores';
import { useBreakpoint } from '@/hooks/use-breakpoint';
import { useFlow } from '@/providers/flow-provider';

interface FlowTabsProps {
    activeTab: string;
    onTabChange: (tab: string) => void;
}

function FlowTabs({ activeTab, onTabChange }: FlowTabsProps) {
    const { isDesktop } = useBreakpoint();
    const { isLoading } = useFlow();

    const previousActiveTabRef = useRef<string>(activeTab);

    useEffect(() => {
        if (activeTab === previousActiveTabRef.current) {
            return;
        }

        previousActiveTabRef.current = activeTab;
    }, [activeTab]);

    return (
        <Tabs
            className="relative flex size-full flex-col"
            onValueChange={onTabChange}
            value={activeTab}
        >
            <div className="max-w-full pr-4">
                <ScrollArea className="w-full pb-3">
                    <TabsList className="flex w-fit">
                        {!isDesktop && <TabsTrigger value="dashboard">Dashboard</TabsTrigger>}
                        {!isDesktop && <TabsTrigger value="automation">Automation</TabsTrigger>}
                        {!isDesktop && <TabsTrigger value="assistant">Assistant</TabsTrigger>}
                        <TabsTrigger value="terminal">Terminal</TabsTrigger>
                        <TabsTrigger value="shell">Shell</TabsTrigger>
                        <TabsTrigger value="tasks">Tasks</TabsTrigger>
                        <TabsTrigger value="agents">Agents</TabsTrigger>
                        <TabsTrigger value="tools">Searches</TabsTrigger>
                        <TabsTrigger value="vectorStores">Vector Store</TabsTrigger>
                        <TabsTrigger value="reports">Reports</TabsTrigger>
                        <TabsTrigger value="files">Files</TabsTrigger>
                        <TabsTrigger value="screenshots">Screenshots</TabsTrigger>
                    </TabsList>
                    <ScrollBar orientation="horizontal" />
                </ScrollArea>
            </div>

            {!isDesktop && (
                <TabsContent
                    className="mt-1 flex-1 overflow-auto"
                    value="automation"
                >
                    <FlowAutomationMessages className="pr-4" />
                </TabsContent>
            )}
            {!isDesktop && (
                <TabsContent
                    className="mt-1 flex-1 overflow-auto"
                    value="assistant"
                >
                    <FlowAssistantMessages className="pr-4" />
                </TabsContent>
            )}
            {!isDesktop && (
                <TabsContent
                    className="mt-1 flex-1 overflow-auto pr-4"
                    value="dashboard"
                >
                    <FlowDashboard />
                </TabsContent>
            )}

            <TabsContent
                className="mt-1 flex-1 overflow-auto"
                value="terminal"
            >
                <FlowTerminal />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="shell"
            >
                <FlowShell />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="tasks"
            >
                <FlowTasks />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="agents"
            >
                <FlowAgents />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="tools"
            >
                <FlowTools />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="vectorStores"
            >
                <FlowVectorStores />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="reports"
            >
                <FlowReports />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="files"
            >
                <FlowFiles />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="screenshots"
            >
                <FlowScreenshots />
            </TabsContent>
            {isLoading && (
                <div
                    aria-label="Loading flow"
                    aria-live="polite"
                    className="bg-background/60 absolute inset-0 z-20 flex flex-col items-center justify-center gap-2"
                    role="status"
                >
                    <Spinner className="text-muted-foreground size-6" />
                    <span className="text-muted-foreground text-xs">Loading flow...</span>
                </div>
            )}
        </Tabs>
    );
}

export default FlowTabs;
