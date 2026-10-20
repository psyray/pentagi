import { ArrowUp, ChevronDown, FileText, Plus, Square, Trash2 } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Spinner } from '@/components/ui/spinner';
import { TextareaAutosize } from '@/components/ui/textarea-autosize';
import { type ReportFragmentFragment } from '@/graphql/types';
import { routes } from '@/lib/routes';
import { cn } from '@/lib/utils';
import { useFlow } from '@/providers/flow-provider';

import FlowReportCreateDialog from './flow-report-create-dialog';
import FlowReportingConversation from './flow-reporting-conversation';
import { formatDuration, formatTokens, isReportActive, reportStatusLabel, reportTookSeconds } from './report-format';
import { useFlowReporting } from './use-flow-reporting';
import { useReportPermissions } from './use-report-permissions';

const TICK_INTERVAL_MS = 15_000;

interface ReportSelectorProps {
    onSelect: (reportId: string) => void;
    reports: readonly ReportFragmentFragment[];
    selected: ReportFragmentFragment;
}

function FlowReportingTab() {
    const { flowId } = useFlow();
    const navigate = useNavigate();
    const {
        addNote,
        checkVision,
        createReport,
        deleteReport,
        groups,
        isNoting,
        loading,
        reports,
        sections,
        selectedReport,
        selectReport,
        stop,
    } = useFlowReporting();

    const [createOpen, setCreateOpen] = useState(false);
    const [note, setNote] = useState('');
    const [stopOpen, setStopOpen] = useState(false);
    const [deleteOpen, setDeleteOpen] = useState(false);
    const permissions = useReportPermissions();
    const containerRef = useRef<HTMLDivElement>(null);
    const endRef = useRef<HTMLDivElement>(null);
    const atBottomRef = useRef(true);

    const messageCount = useMemo(() => groups.reduce((total, group) => total + group.logs.length, 0), [groups]);
    const reportId = selectedReport?.id ?? null;

    const handleScroll = useCallback(() => {
        const container = containerRef.current;

        if (!container) {
            return;
        }

        atBottomRef.current = container.scrollHeight - container.scrollTop - container.clientHeight < 80;
    }, []);

    // Follow the tail while the console is at the bottom; a reader who scrolled up
    // keeps their place as new frames land.
    useEffect(() => {
        if (atBottomRef.current) {
            endRef.current?.scrollIntoView({ block: 'end' });
        }
    }, [messageCount]);

    const handleOpenReport = useCallback(() => {
        if (flowId && reportId) {
            void navigate(routes.flowReportDocument(flowId, reportId));
        }
    }, [flowId, navigate, reportId]);

    const handleSubmitNote = useCallback(
        async (event: React.FormEvent<HTMLFormElement>) => {
            event.preventDefault();

            if (await addNote(note)) {
                setNote('');
            }
        },
        [addNote, note],
    );

    if (!selectedReport) {
        return (
            <div className="flex h-full flex-col">
                <Empty className="flex-1 border-0">
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <FileText />
                        </EmptyMedia>
                        <EmptyTitle>{loading ? 'Loading reports…' : 'No reports yet'}</EmptyTitle>
                        <EmptyDescription>
                            A report can be written from this engagement record at any point in the run.
                        </EmptyDescription>
                    </EmptyHeader>
                    {permissions.canCreate && (
                        <Button
                            disabled={loading}
                            onClick={() => setCreateOpen(true)}
                            variant="outline"
                        >
                            <Plus className="size-4" />
                            Create report
                        </Button>
                    )}
                </Empty>

                <FlowReportCreateDialog
                    onCheckVision={checkVision}
                    onCreate={createReport}
                    onCreated={(report) => selectReport(String(report.id))}
                    onOpenChange={setCreateOpen}
                    open={createOpen}
                />
            </div>
        );
    }

    const active = isReportActive(selectedReport.status);

    return (
        <div className="flex h-full min-h-0 flex-col">
            <div className="bg-background sticky top-0 z-10 flex flex-col gap-2 pb-3">
                <div className="flex items-center gap-2">
                    <ReportSelector
                        onSelect={selectReport}
                        reports={reports}
                        selected={selectedReport}
                    />
                    <div className="ml-auto flex shrink-0 items-center gap-2">
                        {permissions.canDelete && (
                            <Button
                                aria-label="Delete the report"
                                onClick={() => setDeleteOpen(true)}
                                size="icon-sm"
                                title="Delete the report"
                                variant="outline"
                            >
                                <Trash2 />
                            </Button>
                        )}
                        {permissions.canCreate && (
                            <Button
                                onClick={() => setCreateOpen(true)}
                                size="sm"
                                variant="outline"
                            >
                                <Plus className="size-4" />
                                Create report
                            </Button>
                        )}
                    </div>
                </div>
                <ReportStatusLine report={selectedReport} />
            </div>

            <div
                className="min-h-0 flex-1 overflow-y-auto pr-1"
                onScroll={handleScroll}
                ref={containerRef}
            >
                <FlowReportingConversation
                    groups={groups}
                    onOpenReport={handleOpenReport}
                    sections={sections}
                />
                <div ref={endRef} />
            </div>

            <div className="bg-background sticky bottom-0 pt-3">
                <form
                    className="flex flex-col gap-2"
                    onSubmit={handleSubmitNote}
                >
                    <TextareaAutosize
                        aria-label="Add a note to the generation"
                        disabled={!active}
                        maxRows={6}
                        minRows={1}
                        onChange={(event) => setNote(event.target.value)}
                        placeholder={active ? 'Add a note to the generation...' : 'The run has finished.'}
                        value={note}
                    />
                    <div className="flex items-center justify-between gap-2">
                        <span className="text-muted-foreground text-xs">
                            {active ? 'Passed to the sections still to come.' : `${messageCount} messages`}
                        </span>
                        <div className="flex items-center gap-2">
                            {active && (
                                <Button
                                    aria-label="Stop the run"
                                    onClick={() => setStopOpen(true)}
                                    size="icon-sm"
                                    title="Stop the run"
                                    type="button"
                                    variant="destructive"
                                >
                                    <Square />
                                </Button>
                            )}
                            <Button
                                aria-label="Send the note"
                                disabled={!active || !note.trim() || isNoting}
                                size="icon-sm"
                                title="Send the note"
                                type="submit"
                            >
                                {isNoting ? <Spinner className="size-4" /> : <ArrowUp />}
                            </Button>
                        </div>
                    </div>
                </form>
            </div>

            <FlowReportCreateDialog
                onCheckVision={checkVision}
                onCreate={createReport}
                onCreated={(report) => selectReport(String(report.id))}
                onOpenChange={setCreateOpen}
                open={createOpen}
            />

            <ConfirmationDialog
                confirmText="Stop"
                description="Stop the generation? The sections already written are kept; the ones still to come stay unwritten."
                handleConfirm={async () => {
                    await stop();
                }}
                handleOpenChange={setStopOpen}
                isOpen={stopOpen}
                itemName={selectedReport.title || 'this report'}
                itemType="report"
                title="Stop the run"
            />

            <ConfirmationDialog
                confirmText="Delete"
                description={
                    active
                        ? `Delete "${selectedReport.title || 'this report'}"? The run is stopped and the document, its sections and its logs are removed for good.`
                        : `Delete "${selectedReport.title || 'this report'}"? The document, its sections and its logs are removed for good.`
                }
                handleConfirm={async () => {
                    await deleteReport(selectedReport);
                }}
                handleOpenChange={setDeleteOpen}
                isOpen={deleteOpen}
                itemName={selectedReport.title || 'this report'}
                itemType="report"
                title="Delete report"
            />
        </div>
    );
}

function ReportSelector({ onSelect, reports, selected }: ReportSelectorProps) {
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button
                    className="min-w-0"
                    size="sm"
                    variant="outline"
                >
                    <FileText className="size-4 shrink-0" />
                    <span className="truncate">{selected.title || 'Untitled report'}</span>
                    <ChevronDown className="size-4 shrink-0" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
                align="start"
                className="max-w-96"
            >
                <DropdownMenuLabel>Reports</DropdownMenuLabel>
                {reports.map((report) => (
                    <DropdownMenuItem
                        key={report.id}
                        onSelect={() => onSelect(String(report.id))}
                    >
                        <span className="min-w-0 flex-1 truncate">{report.title || 'Untitled report'}</span>
                        <span className="text-muted-foreground ml-3 shrink-0 text-xs">
                            {reportStatusLabel(report.status)}
                        </span>
                    </DropdownMenuItem>
                ))}
            </DropdownMenuContent>
        </DropdownMenu>
    );
}

function ReportStatusLine({ report }: { report: ReportFragmentFragment }) {
    const active = isReportActive(report.status);
    const [now, setNow] = useState(() => Date.now());

    useEffect(() => {
        if (!active) {
            return;
        }

        const interval = setInterval(() => setNow(Date.now()), TICK_INTERVAL_MS);

        return () => clearInterval(interval);
    }, [active]);

    const tookSeconds = reportTookSeconds(report, now);
    const budgetSeconds = report.controlBudgetSeconds + report.contentBudgetSeconds;
    const progress =
        report.sectionsTotal > 0 ? Math.min(100, Math.round((report.sectionsDone / report.sectionsTotal) * 100)) : 0;
    const sections =
        report.sectionsTotal > 0
            ? `${report.sectionsDone} of ${report.sectionsTotal}`
            : report.sectionsEstimate > 0
              ? `~${report.sectionsEstimate}`
              : 'estimating';
    const created = new Date(report.createdAt);

    return (
        <div className="flex flex-col gap-1.5">
            <div className="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                <span className="flex items-center gap-1">
                    Created
                    <Badge
                        className="font-normal"
                        variant="secondary"
                    >
                        {formatClock(created)}
                    </Badge>
                </span>
                <span className="flex items-center gap-1">
                    Status
                    <Badge
                        className="font-normal"
                        variant="secondary"
                    >
                        {reportStatusLabel(report.status)}
                    </Badge>
                </span>
                <span className="flex items-center gap-1">
                    Sections
                    <Badge
                        className="font-normal"
                        variant="secondary"
                    >
                        {sections}
                    </Badge>
                </span>
                <span className="flex items-center gap-1">
                    Took
                    <Badge
                        className="font-normal"
                        variant="secondary"
                    >
                        {budgetSeconds > 0
                            ? `${formatDuration(tookSeconds)} of ${formatDuration(budgetSeconds)}`
                            : formatDuration(tookSeconds)}
                    </Badge>
                </span>
                <span className="flex items-center gap-1">
                    Tokens
                    <Badge
                        className="font-normal"
                        variant="secondary"
                    >
                        {formatTokens(report.tokensIn + report.tokensOut)}
                    </Badge>
                </span>
            </div>
            <div className="bg-muted relative h-1.5 overflow-hidden rounded-full">
                <div
                    className={cn('h-full rounded-full transition-all', active ? 'bg-primary/70' : 'bg-primary')}
                    style={{ width: `${progress}%` }}
                />
                {active && (
                    <div
                        aria-hidden
                        className="pointer-events-none absolute inset-0 bg-[repeating-linear-gradient(45deg,transparent,transparent_6px,rgba(255,255,255,0.18)_6px,rgba(255,255,255,0.18)_12px)]"
                    />
                )}
            </div>
        </div>
    );
}

const formatClock = (date: Date): string =>
    Number.isNaN(date.getTime()) ? '' : date.toLocaleTimeString([], { hour12: false });

export default FlowReportingTab;
