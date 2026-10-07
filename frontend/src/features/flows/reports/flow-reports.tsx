import { FileText, Plus } from 'lucide-react';
import { useCallback, useState } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { ScrollArea } from '@/components/ui/scroll-area';

import FlowReportCard from './flow-report-card';
import FlowReportCreateDialog from './flow-report-create-dialog';
import { type ReportProgress, useFlowReports } from './use-flow-reports';

interface FlowReportsProps {
    className?: string;
}

function FlowReports({ className }: FlowReportsProps) {
    const { createReport, loading, reports } = useFlowReports();
    const [createOpen, setCreateOpen] = useState(false);

    const handleOpen = useCallback((report: ReportProgress) => {
        // The full-page document view arrives with the next iteration; the
        // list and its live progress are the P2 surface.
        toast.info('The full-page report view is on its way (next iteration).', { description: report.title });
    }, []);

    const handleDownload = useCallback((_report: ReportProgress) => {
        toast.info('Report download arrives with the full-page view (next iteration).');
    }, []);

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
                onCreate={createReport}
                onOpenChange={setCreateOpen}
                open={createOpen}
            />
        </div>
    );
}

export default FlowReports;
