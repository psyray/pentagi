import { useApolloClient } from '@apollo/client/react';
import { FileText, Plus } from 'lucide-react';
import { useCallback, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { ScrollArea } from '@/components/ui/scroll-area';
import { ReportVisionDocument } from '@/graphql/types';
import { Log } from '@/lib/log';
import { routes } from '@/lib/routes';
import { useFlow } from '@/providers/flow-provider';

import FlowReportCard from './flow-report-card';
import FlowReportCreateDialog from './flow-report-create-dialog';
import { downloadReportMarkdown, fetchReportDocument } from './report-download';
import { type ReportProgress, useFlowReports } from './use-flow-reports';

interface FlowReportsProps {
    className?: string;
}

function FlowReports({ className }: FlowReportsProps) {
    const { createReport, loading, reports } = useFlowReports();
    const { flowId } = useFlow();
    const navigate = useNavigate();
    const client = useApolloClient();
    const [createOpen, setCreateOpen] = useState(false);

    const handleOpen = useCallback(
        (report: ReportProgress) => {
            if (!flowId) {
                return;
            }

            void navigate(routes.flowReportDocument(flowId, report.id));
        },
        [flowId, navigate],
    );

    const handleDownload = useCallback(
        async (report: ReportProgress) => {
            try {
                const content = await fetchReportDocument(client, report.id);

                if (!content.trim()) {
                    toast.error('The report document is empty');

                    return;
                }

                downloadReportMarkdown(report, content);
            } catch (error) {
                Log.error('Failed to download the report:', error);
                toast.error('Failed to download the report');
            }
        },
        [client],
    );

    // Probed from the dialog before creating: warns when the chosen model
    // cannot read images, so screenshots would not be analysed (plan § 6.10).
    const checkVision = useCallback(
        async (providerName: null | string): Promise<{ error?: null | string; vision: boolean }> => {
            if (!flowId) {
                return { vision: false };
            }

            try {
                const response = await client.query({
                    fetchPolicy: 'network-only',
                    query: ReportVisionDocument,
                    variables: { flowId, providerName },
                });

                return response.data?.reportVision ?? { vision: false };
            } catch (error) {
                Log.error('Failed to probe the report provider vision:', error);

                return { error: 'the provider probe failed', vision: false };
            }
        },
        [client, flowId],
    );

    return (
        <div className={`flex size-full flex-col ${className ?? ''}`}>
            <div className="flex items-center justify-between gap-2 pb-3">
                <div className="text-muted-foreground text-sm">
                    {loading ? 'Loading…' : `${reports.length} report${reports.length === 1 ? '' : 's'}`}
                </div>
                <Button
                    disabled={loading}
                    onClick={() => setCreateOpen(true)}
                    size="sm"
                >
                    <Plus className="size-4" />
                    Create report
                </Button>
            </div>

            {reports.length === 0 ? (
                <Empty className="flex-1 border-0">
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <FileText />
                        </EmptyMedia>
                        <EmptyTitle>{loading ? 'Loading reports…' : 'No reports yet'}</EmptyTitle>
                        <EmptyDescription>
                            {loading
                                ? 'Checking what has already been written for this engagement.'
                                : 'No reports have been written for this engagement yet. One can be started at any point in the run.'}
                        </EmptyDescription>
                    </EmptyHeader>
                    {!loading && (
                        <EmptyContent>
                            <Button
                                disabled={loading}
                                onClick={() => setCreateOpen(true)}
                                variant="outline"
                            >
                                <Plus className="size-4" />
                                Create report
                            </Button>
                        </EmptyContent>
                    )}
                </Empty>
            ) : (
                <ScrollArea className="min-h-0 flex-1 pr-3">
                    <div className="flex flex-col gap-3 pb-6">
                        {reports.map((report) => (
                            <FlowReportCard
                                key={report.id}
                                onDownload={handleDownload}
                                onOpen={handleOpen}
                                report={report}
                            />
                        ))}
                    </div>
                </ScrollArea>
            )}

            {loading && (
                <div className="text-muted-foreground flex items-center justify-center py-2 text-xs">
                    Refreshing reports…
                </div>
            )}

            <FlowReportCreateDialog
                onCheckVision={checkVision}
                onCreate={createReport}
                onOpenChange={setCreateOpen}
                open={createOpen}
            />
        </div>
    );
}

export default FlowReports;
