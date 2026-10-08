import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import FlowDashboard from '@/features/flows/dashboard/flow-dashboard';
import FlowAssistantMessages from '@/features/flows/messages/flow-assistant-messages';
import FlowAutomationMessages from '@/features/flows/messages/flow-automation-messages';
import FlowReportingTab from '@/features/flows/reports/flow-reporting-tab';
import { useFlowTabDetection } from '@/hooks/use-flow-tab-detection';

function FlowCentralTabs() {
    const { handleTabChange, resolvedTab } = useFlowTabDetection();

    return (
        <Tabs
            activationMode="manual"
            className="flex size-full flex-col"
            onValueChange={handleTabChange}
            value={resolvedTab}
        >
            <div className="max-w-full">
                <ScrollArea className="w-full pb-3">
                    <TabsList className="flex w-fit">
                        <TabsTrigger value="automation">Automation</TabsTrigger>
                        <TabsTrigger value="assistant">Assistant</TabsTrigger>
                        <TabsTrigger value="reporting">Reporting</TabsTrigger>
                        <TabsTrigger value="dashboard">Dashboard</TabsTrigger>
                    </TabsList>
                    <ScrollBar orientation="horizontal" />
                </ScrollArea>
            </div>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="automation"
            >
                <FlowAutomationMessages />
            </TabsContent>
            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="assistant"
            >
                <FlowAssistantMessages />
            </TabsContent>
            <TabsContent
                className="mt-1 flex min-h-0 flex-1 flex-col overflow-hidden pr-4"
                value="reporting"
            >
                <FlowReportingTab />
            </TabsContent>
            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="dashboard"
            >
                <FlowDashboard />
            </TabsContent>
        </Tabs>
    );
}

export default FlowCentralTabs;
