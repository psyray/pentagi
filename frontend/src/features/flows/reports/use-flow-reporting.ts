import { useMutation, useQuery, useSubscription } from '@apollo/client/react';
import { useCallback, useMemo, useState } from 'react';
import { toast } from 'sonner';

import {
    PutReportNoteDocument,
    ReportLogAddedDocument,
    ReportLogsDocument,
    ReportSectionsDocument,
    StopReportDocument,
} from '@/graphql/types';
import { Log } from '@/lib/log';
import { useFlow } from '@/providers/flow-provider';

import { groupReportLogs } from './flow-reporting-groups';
import { isReportActive } from './report-format';
import { findReportById } from './report-outline';
import { useFlowReports } from './use-flow-reports';

/**
 * The central Reporting console: one report selected at a time, its generation
 * conversation (reportLogs, live through the flow-scoped subscription) grouped
 * by section, plus the operator note and stop controls.
 */
export function useFlowReporting() {
    const { flowId } = useFlow();
    const { checkVision, createReport, loading: reportsLoading, reports } = useFlowReports();
    const [requestedId, setRequestedId] = useState<null | string>(null);

    // Keep an explicit pick while the report exists; otherwise follow the run in
    // progress, falling back to the most recently created report.
    const selectedReport = useMemo(() => {
        if (requestedId) {
            const picked = findReportById(reports, requestedId);

            if (picked) {
                return picked;
            }
        }

        return reports.find((report) => isReportActive(report.status)) ?? reports[0] ?? null;
    }, [reports, requestedId]);

    const reportId = selectedReport?.id ?? null;

    const logsQuery = useQuery(ReportLogsDocument, {
        skip: !reportId,
        variables: { reportId: reportId ?? '' },
    });
    const sectionsQuery = useQuery(ReportSectionsDocument, {
        skip: !reportId,
        variables: { reportId: reportId ?? '' },
    });

    // The subscription cache link routes reportLogAdded into the report's own
    // reportLogs list, so no onData write is needed here.
    useSubscription(ReportLogAddedDocument, { skip: !flowId, variables: { flowId: flowId ?? '' } });

    const [putNote, { loading: isNoting }] = useMutation(PutReportNoteDocument);
    const [stopReport, { loading: isStopping }] = useMutation(StopReportDocument);

    const logs = useMemo(() => logsQuery.data?.reportLogs ?? [], [logsQuery.data?.reportLogs]);
    const sections = useMemo(() => sectionsQuery.data?.reportSections ?? [], [sectionsQuery.data?.reportSections]);
    const groups = useMemo(() => groupReportLogs(logs, sections), [logs, sections]);

    const addNote = useCallback(
        async (note: string): Promise<boolean> => {
            const trimmed = note.trim();

            if (!reportId || !trimmed) {
                return false;
            }

            try {
                await putNote({ variables: { note: trimmed, reportId } });

                return true;
            } catch (error) {
                Log.error('Failed to add the report note:', error);
                toast.error('Failed to add the note');

                return false;
            }
        },
        [putNote, reportId],
    );

    const stop = useCallback(async (): Promise<boolean> => {
        if (!reportId) {
            return false;
        }

        try {
            await stopReport({ variables: { reportId } });

            return true;
        } catch (error) {
            Log.error('Failed to stop the report:', error);
            toast.error('Failed to stop the report');

            return false;
        }
    }, [reportId, stopReport]);

    return {
        addNote,
        checkVision,
        createReport,
        groups,
        isNoting,
        isStopping,
        loading: reportsLoading,
        reports,
        sections,
        selectedReport,
        selectReport: setRequestedId,
        stop,
    };
}
