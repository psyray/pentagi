import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
import { Spinner } from '@/components/ui/spinner';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import FlowDashboard from '@/features/flows/dashboard/flow-dashboard';
import FlowAssistantMessages from '@/features/flows/messages/flow-assistant-messages';
import FlowAutomationMessages from '@/features/flows/messages/flow-automation-messages';
import { useFlowTabDetection } from '@/hooks/use-flow-tab-detection';
import { useFlow } from '@/providers/flow-provider';

function FlowCentralTabs() {
    const { handleTabChange, resolvedTab } = useFlowTabDetection();
    const { isLoading } = useFlow();

    return (
        <Tabs
            activationMode="manual"
            className="relative flex size-full flex-col"
            onValueChange={handleTabChange}
            value={resolvedTab}
        >
            <div className="max-w-full">
                <ScrollArea className="w-full pb-3">
                    <TabsList className="flex w-fit">
                        <TabsTrigger value="dashboard">Dashboard</TabsTrigger>
                        <TabsTrigger value="automation">Automation</TabsTrigger>
                        <TabsTrigger value="assistant">Assistant</TabsTrigger>
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
                className="mt-1 flex-1 overflow-auto pr-4"
                value="dashboard"
            >
                <FlowDashboard />
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

export default FlowCentralTabs;
