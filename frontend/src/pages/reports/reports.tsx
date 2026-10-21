import type { ColumnDef } from '@tanstack/react-table';

import { Ellipsis, ExternalLink, RotateCcw, ScrollText, Trash } from 'lucide-react';
import { useCallback, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import type { BadgeVariant } from '@/components/ui/badge';
import type { ReportProgress } from '@/features/flows/reports/use-flow-reports';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import { ErrorState } from '@/components/shared/error-state';
import { LoadingState } from '@/components/shared/loading-state';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ContextMenuItem, ContextMenuSeparator } from '@/components/ui/context-menu';
import { DataTable, DataTableColumnHeader } from '@/components/ui/data-table';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Spinner } from '@/components/ui/spinner';
import {
    formatDuration,
    isReportActive,
    isReportResumable,
    reportStatusLabel,
    reportTookSeconds,
} from '@/features/flows/reports/report-format';
import { useAllReports } from '@/features/flows/reports/use-all-reports';
import { useReportPermissions } from '@/features/flows/reports/use-report-permissions';
import { ReportStatusType } from '@/graphql/types';
import { useTableState } from '@/hooks/use-table-state';
import { routes } from '@/lib/routes';
import { formatDate } from '@/lib/utils/format';

const statusVariant: Record<ReportStatusType, BadgeVariant> = {
    [ReportStatusType.Canceled]: 'secondary',
    [ReportStatusType.Completed]: 'green',
    [ReportStatusType.Created]: 'blue',
    [ReportStatusType.Failed]: 'red',
    [ReportStatusType.Generating]: 'blue',
    [ReportStatusType.Planning]: 'blue',
};

const sectionsLabel = (report: ReportProgress): string => {
    if (report.sectionsTotal > 0) {
        return `${report.sectionsDone} of ${report.sectionsTotal}`;
    }

    return report.sectionsEstimate > 0 ? `~${report.sectionsEstimate}` : '-';
};

function Reports() {
    const navigate = useNavigate();
    const { deleteReport, error, loading, refetch, reports, resumeReport } = useAllReports();
    const permissions = useReportPermissions();
    const { filter, setFilter } = useTableState();
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deletingReport, setDeletingReport] = useState<null | ReportProgress>(null);

    const openReport = useCallback(
        (report: ReportProgress) => {
            void navigate(routes.report(report.id));
        },
        [navigate],
    );

    const handleDelete = useCallback(async () => {
        if (deletingReport && (await deleteReport(deletingReport))) {
            toast.success('Report deleted');
            setDeletingReport(null);
        }
    }, [deleteReport, deletingReport]);

    const handleResume = useCallback(
        async (report: ReportProgress) => {
            if (await resumeReport(report)) {
                toast.success('Report resumed');
            }
        },
        [resumeReport],
    );

    const columns: ColumnDef<ReportProgress>[] = [
        {
            accessorKey: 'status',
            cell: ({ row }) => {
                const report = row.original;

                return (
                    <Badge
                        className="shrink-0 whitespace-nowrap"
                        variant={statusVariant[report.status]}
                    >
                        {isReportActive(report.status) && <Spinner variant="circle" />}
                        {reportStatusLabel(report.status)}
                    </Badge>
                );
            },
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title="Status"
                />
            ),
            maxSize: 140,
            meta: { columnMenuLabel: 'Status', searchable: true },
            minSize: 90,
            size: 110,
        },
        {
            accessorKey: 'title',
            cell: ({ row }) => {
                const report = row.original;
                const title = report.title || 'Untitled report';

                return (
                    <div className="flex min-w-0 items-center gap-2 overflow-hidden">
                        <span
                            className="truncate font-medium"
                            title={title}
                        >
                            {title}
                        </span>
                        <Badge
                            className="hidden shrink-0 font-mono text-xs sm:inline-flex"
                            variant="secondary"
                        >
                            {report.templateKey.replace(/^.*\//, '')}
                        </Badge>
                    </div>
                );
            },
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title="Title"
                />
            ),
            meta: { columnMenuLabel: 'Title', searchable: true },
            minSize: 200,
            size: 320,
        },
        {
            accessorKey: 'flowId',
            cell: ({ row }) => {
                const { flowId } = row.original;

                if (!flowId) {
                    return <span className="text-muted-foreground text-sm">-</span>;
                }

                return (
                    <Link
                        className="text-primary truncate text-sm hover:underline"
                        onClick={(event) => event.stopPropagation()}
                        title={`Open flow #${flowId}`}
                        to={routes.flow(flowId)}
                    >
                        Flow #{flowId}
                    </Link>
                );
            },
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title="Flow"
                />
            ),
            maxSize: 160,
            meta: { columnMenuLabel: 'Flow' },
            minSize: 90,
            size: 120,
        },
        {
            accessorKey: 'providerName',
            cell: ({ row }) => (
                <span
                    className="text-muted-foreground truncate text-sm"
                    title={row.original.providerName}
                >
                    {row.original.providerName}
                </span>
            ),
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title="Provider"
                />
            ),
            meta: { columnMenuLabel: 'Provider', searchable: true },
            minSize: 120,
            size: 160,
        },
        {
            accessorKey: 'sectionsDone',
            cell: ({ row }) => <span className="text-sm">{sectionsLabel(row.original)}</span>,
            enableSorting: false,
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title="Sections"
                />
            ),
            maxSize: 120,
            meta: { columnMenuLabel: 'Sections' },
            minSize: 80,
            size: 100,
        },
        {
            accessorKey: 'createdAt',
            cell: ({ row }) => {
                const report = row.original;
                const tookSeconds = reportTookSeconds(report, Date.now());

                return (
                    <div className="flex flex-col">
                        <span className="text-sm">{formatDate(new Date(report.createdAt))}</span>
                        <span className="text-muted-foreground text-xs">
                            {tookSeconds > 0 ? formatDuration(tookSeconds) : '-'}
                        </span>
                    </div>
                );
            },
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title="Created"
                />
            ),
            maxSize: 180,
            meta: { columnMenuLabel: 'Created' },
            minSize: 120,
            size: 150,
        },
        {
            cell: ({ row }) => {
                const report = row.original;

                return (
                    <div className="flex items-center justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                        <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                                <Button
                                    aria-label="Open menu"
                                    className="size-8 p-0"
                                    onClick={(event) => event.stopPropagation()}
                                    variant="ghost"
                                >
                                    <Ellipsis />
                                </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent
                                align="end"
                                className="min-w-24"
                                onClick={(event) => event.stopPropagation()}
                            >
                                <DropdownMenuItem onClick={() => openReport(report)}>
                                    <ExternalLink />
                                    Open
                                </DropdownMenuItem>
                                {permissions.canEdit && isReportResumable(report) && (
                                    <DropdownMenuItem
                                        onClick={() => {
                                            void handleResume(report);
                                        }}
                                    >
                                        <RotateCcw />
                                        Resume
                                    </DropdownMenuItem>
                                )}
                                {permissions.canDelete && (
                                    <>
                                        <DropdownMenuSeparator />
                                        <DropdownMenuItem
                                            onClick={() => {
                                                setDeletingReport(report);
                                                setDeleteOpen(true);
                                            }}
                                        >
                                            <Trash />
                                            Delete
                                        </DropdownMenuItem>
                                    </>
                                )}
                            </DropdownMenuContent>
                        </DropdownMenu>
                    </div>
                );
            },
            enableHiding: false,
            header: () => null,
            id: 'actions',
            maxSize: 70,
            meta: { preventRowClick: true },
            minSize: 50,
            size: 60,
        },
    ];

    const renderRowContextMenu = (report: ReportProgress) => (
        <>
            <ContextMenuItem onClick={() => openReport(report)}>
                <ExternalLink />
                Open
            </ContextMenuItem>
            {permissions.canEdit && isReportResumable(report) && (
                <ContextMenuItem
                    onClick={() => {
                        void handleResume(report);
                    }}
                >
                    <RotateCcw />
                    Resume
                </ContextMenuItem>
            )}
            {permissions.canDelete && (
                <>
                    <ContextMenuSeparator />
                    <ContextMenuItem
                        onClick={() => {
                            setDeletingReport(report);
                            setDeleteOpen(true);
                        }}
                    >
                        <Trash />
                        Delete
                    </ContextMenuItem>
                </>
            )}
        </>
    );

    const pageHeader = (
        <AppHeader>
            <AppHeaderContent>
                <AppHeaderTitle icon={<ScrollText className="size-4 shrink-0" />}>Reports</AppHeaderTitle>
            </AppHeaderContent>
        </AppHeader>
    );

    if (loading && !reports.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <LoadingState
                        description="Please wait while we fetch your reports"
                        title="Loading reports..."
                    />
                </div>
            </>
        );
    }

    // Error surface only when there's no data — a failed background refetch must not blank a working list.
    if (error && !reports.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <ErrorState
                        message={error.message}
                        onRetry={refetch}
                        title="Error loading reports"
                    />
                </div>
            </>
        );
    }

    if (!reports.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <Empty>
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <ScrollText />
                            </EmptyMedia>
                            <EmptyTitle>No reports yet</EmptyTitle>
                            <EmptyDescription>
                                Reports are generated from a flow: open a flow and create one from its Reports tab
                            </EmptyDescription>
                        </EmptyHeader>
                    </Empty>
                </div>
            </>
        );
    }

    return (
        <>
            {pageHeader}
            <div className="flex flex-col gap-4 p-4 pt-0">
                <DataTable
                    columns={columns}
                    data={reports}
                    empty={{ entityName: 'reports' }}
                    filterPlaceholder="Filter reports..."
                    filterValue={filter}
                    label="Reports"
                    onFilterChange={setFilter}
                    onRowClick={openReport}
                    renderRowContextMenu={renderRowContextMenu}
                />

                <ConfirmationDialog
                    cancelText="Cancel"
                    confirmText="Delete"
                    description={`Delete "${deletingReport?.title || 'this report'}"? The document, its sections and its logs are removed for good.`}
                    handleConfirm={handleDelete}
                    handleOpenChange={setDeleteOpen}
                    isOpen={deleteOpen}
                    itemName={deletingReport?.title || 'this report'}
                    itemType="report"
                    title="Delete report"
                />
            </div>
        </>
    );
}

export default Reports;
