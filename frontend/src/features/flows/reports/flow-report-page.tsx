import { RotateCcw, Trash2 } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { toast } from 'sonner';

import { AppHeader, AppHeaderActions, AppHeaderContent } from '@/components/layouts/app/app-header';
import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import {
    Breadcrumb,
    BreadcrumbItem,
    BreadcrumbLink,
    BreadcrumbList,
    BreadcrumbPage,
    BreadcrumbSeparator,
} from '@/components/ui/breadcrumb';
import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { ReportSectionStatusType } from '@/graphql/types';
import { routes } from '@/lib/routes';

import FlowReportBranding from './flow-report-branding';
import FlowReportDownload from './flow-report-download';
import FlowReportRun from './flow-report-run';
import FlowReportSection from './flow-report-section';
import FlowReportToc from './flow-report-toc';
import { formatDuration, isReportActive, isSectionResumable } from './report-format';
import { buildReportOutline, flattenReportOutline } from './report-outline';
import { useFlowReportDocument } from './use-flow-report-document';
import { useFlowReportEditor } from './use-flow-report-editor';
import { useReportPermissions } from './use-report-permissions';

function FlowReportPage() {
    const { flowId } = useParams<{ flowId: string }>();
    const navigate = useNavigate();
    const { download, downloading, loading, remove, report, reportId, resume, sections } = useFlowReportDocument();
    const editor = useFlowReportEditor(reportId || null);
    const [deleteOpen, setDeleteOpen] = useState(false);
    const permissions = useReportPermissions();

    const handleSelectSection = useCallback((sectionId: string) => {
        document.getElementById(`report-section-${sectionId}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }, []);

    const handleDelete = useCallback(async () => {
        if (await remove()) {
            toast.success('Report deleted');
            void navigate(flowId ? routes.flow(flowId) : routes.flows);
        }
    }, [flowId, navigate, remove]);

    const handleResume = useCallback(async () => {
        if (await resume()) {
            toast.success('Report resumed');
        }
    }, [resume]);

    const outline = useMemo(() => buildReportOutline(sections), [sections]);
    const flat = useMemo(() => flattenReportOutline(outline), [outline]);

    const activeSectionId = useMemo(
        () => flat.find((node) => node.section.status === ReportSectionStatusType.Writing)?.section.id ?? null,
        [flat],
    );

    const progress =
        report && report.sectionsTotal > 0
            ? Math.min(100, Math.round((report.sectionsDone / report.sectionsTotal) * 100))
            : 0;
    const active = report ? isReportActive(report.status) : false;
    const canEdit = !!report && permissions.canEdit;
    // Precise on the page: the loaded sections tell whether the writer still
    // owes content (the row counters do not account for hand-added sections).
    const resumable = !!report && !active && sections.some((section) => isSectionResumable(section.status));
    const activeTitle = flat.find((node) => node.section.id === activeSectionId)?.section.title ?? null;

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <Breadcrumb className="min-w-0 flex-1">
                        <BreadcrumbList className="min-w-0 flex-nowrap">
                            <BreadcrumbItem className="min-w-0">
                                <BreadcrumbLink asChild>
                                    <Link
                                        className="truncate"
                                        to={flowId ? routes.flow(flowId) : routes.flows}
                                    >
                                        Reports
                                    </Link>
                                </BreadcrumbLink>
                            </BreadcrumbItem>
                            <BreadcrumbSeparator />
                            <BreadcrumbItem className="min-w-0">
                                <BreadcrumbPage className="truncate">
                                    {report ? report.title || 'Untitled report' : 'Report'}
                                </BreadcrumbPage>
                            </BreadcrumbItem>
                        </BreadcrumbList>
                    </Breadcrumb>
                </AppHeaderContent>
                <AppHeaderActions>
                    {resumable && permissions.canEdit && (
                        <Button
                            aria-label="Resume report"
                            onClick={() => {
                                void handleResume();
                            }}
                            size="sm"
                            title="Resume the report"
                            variant="outline"
                        >
                            <RotateCcw className="size-4" />
                            Resume
                        </Button>
                    )}
                    <FlowReportDownload
                        canDownload={!!report && !active}
                        downloading={downloading}
                        onDownload={download}
                    />
                    {permissions.canDelete && (
                        <Button
                            aria-label="Delete report"
                            disabled={!report}
                            onClick={() => setDeleteOpen(true)}
                            size="sm"
                            title="Delete report"
                            variant="outline"
                        >
                            <Trash2 className="size-4" />
                            Delete
                        </Button>
                    )}
                </AppHeaderActions>
            </AppHeader>

            {loading ? (
                <div className="flex flex-1 items-center justify-center">
                    <Spinner className="text-muted-foreground size-6" />
                </div>
            ) : !report ? (
                <div className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
                    This report could not be found.
                </div>
            ) : (
                <div className="flex h-[calc(100dvh-3rem)] min-h-0">
                    <aside className="flex w-[420px] min-w-0 shrink-0 flex-col gap-4 overflow-auto border-r p-4">
                        <div className="flex flex-col gap-2">
                            <div className="flex items-baseline justify-between text-xs">
                                <span className="font-medium">
                                    {report.sectionsTotal > 0
                                        ? `${report.sectionsDone} of ${report.sectionsTotal} sections`
                                        : `${report.sectionsEstimate} sections (estimating)`}
                                </span>
                                <span className="text-muted-foreground">
                                    {report.finishedAt
                                        ? formatDuration(
                                              (new Date(report.finishedAt).getTime() -
                                                  new Date(report.createdAt).getTime()) /
                                                  1000,
                                          )
                                        : active
                                          ? 'running'
                                          : ''}
                                </span>
                            </div>
                            <div className="bg-muted relative h-2 overflow-hidden rounded-full">
                                <div
                                    className="bg-primary h-full rounded-full transition-all"
                                    style={{ width: `${progress}%` }}
                                />
                            </div>
                            {active && activeTitle && (
                                <div className="text-muted-foreground flex items-center gap-2 truncate text-xs">
                                    <Spinner className="size-3 shrink-0" />
                                    <span className="truncate">{activeTitle}</span>
                                </div>
                            )}
                        </div>

                        <FlowReportRun report={report} />

                        <FlowReportToc
                            activeSectionId={activeSectionId}
                            onSelectSection={handleSelectSection}
                            outline={outline}
                        />

                        <FlowReportBranding
                            flowId={flowId ?? null}
                            reportId={reportId || null}
                        />
                    </aside>

                    <div className="min-w-0 flex-1 overflow-auto">
                        <div className="mx-auto w-[95%] max-w-[1500px] p-8 xl:w-[80%]">
                            <h1 className="mb-6 text-2xl font-bold">{report.title || 'Untitled report'}</h1>
                            {outline.map((node) => (
                                <FlowReportSection
                                    active={active}
                                    canEdit={canEdit}
                                    depth={0}
                                    editor={editor}
                                    key={node.section.id}
                                    node={node}
                                    sections={sections}
                                />
                            ))}
                        </div>
                    </div>
                </div>
            )}

            <ConfirmationDialog
                confirmText="Delete"
                description={
                    active
                        ? `Delete "${report?.title || 'this report'}"? The run is stopped and the document, its sections and its logs are removed for good.`
                        : `Delete "${report?.title || 'this report'}"? The document, its sections and its logs are removed for good.`
                }
                handleConfirm={handleDelete}
                handleOpenChange={setDeleteOpen}
                isOpen={deleteOpen}
                itemName={report?.title || 'this report'}
                itemType="report"
                title="Delete report"
            />
        </>
    );
}

export default FlowReportPage;
