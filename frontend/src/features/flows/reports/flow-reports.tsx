import { useApolloClient, useSubscription } from '@apollo/client/react';
import { FileText, Plus } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { ScrollArea } from '@/components/ui/scroll-area';
import { ReportSectionUpdatedDocument } from '@/graphql/types';
import { Log } from '@/lib/log';
import { routes } from '@/lib/routes';
import { useFlow } from '@/providers/flow-provider';

import FlowReportCard from './flow-report-card';
import FlowReportCreateDialog from './flow-report-create-dialog';
import { downloadReportMarkdown, fetchReportDocument } from './report-download';
import { type ReportProgress, useFlowReports } from './use-flow-reports';
import { useReportPermissions } from './use-report-permissions';

interface FlowReportsProps {
    className?: string;
}

function FlowReports({ className }: FlowReportsProps) {
    const { checkVision, createReport, deleteReport, loading, reports } = useFlowReports();
    const permissions = useReportPermissions();
    const { flowId } = useFlow();
    const navigate = useNavigate();
    const client = useApolloClient();
    const [createOpen, setCreateOpen] = useState(false);

    // Keeps whatever expanded card's section list live; the cache link routes the
    // frame into reportSections(reportId) by the payload's reportId.
    const subscriptionVariables = useMemo(() => ({ flowId: flowId ?? '' }), [flowId]);

    useSubscription(ReportSectionUpdatedDocument, { skip: !flowId, variables: subscriptionVariables });

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

    return (
        <div className={`flex size-full min-w-0 flex-col ${className ?? ''}`}>
            <div className="flex items-center justify-between gap-2 pb-3">
                <div className="text-muted-foreground text-sm">
                    {loading ? 'Loading…' : `${reports.length} report${reports.length === 1 ? '' : 's'}`}
                </div>
                {permissions.canCreate && (
                    <Button
                        disabled={loading}
                        onClick={() => setCreateOpen(true)}
                        size="sm"
                    >
                        <Plus className="size-4" />
                        Create report
                    </Button>
                )}
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
                    {!loading && permissions.canCreate && (
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
                <ScrollArea className="min-h-0 min-w-0 flex-1 pr-3 [&_[data-slot=scroll-area-viewport]>div]:block!">
                    {/* Radix wraps the children in a `display: table` box, which never
                        shrinks below their min-content: on a narrow panel the card
                        overflowed and its right side was clipped. Forcing that wrapper
                        back to a block keeps the content at the viewport width. */}
                    <div className="flex min-w-0 flex-col gap-3 pb-6">
                        {reports.map((report) => (
                            <FlowReportCard
                                canDelete={permissions.canDelete}
                                key={report.id}
                                onDelete={deleteReport}
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
