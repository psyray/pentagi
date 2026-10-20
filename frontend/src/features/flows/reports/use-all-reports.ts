import { useMutation, useQuery } from '@apollo/client/react';
import { useCallback, useEffect, useMemo } from 'react';
import { toast } from 'sonner';

import { AllReportsDocument, DeleteReportDocument, ResumeReportDocument } from '@/graphql/types';
import { Log } from '@/lib/log';

import type { ReportProgress } from './use-flow-reports';

// There is no user-keyed subscription channel: the global list stays fresh
// through a light poll, a refetch on mount and one on window focus.
const REFETCH_INTERVAL_MS = 20_000;

/**
 * Cross-flow view of the caller's reports (global Reports page, L5). The
 * flow-scoped `reportUpdated` frames never reach this list, so freshness comes
 * from polling and focus refetches rather than a subscription.
 */
export function useAllReports() {
    const { data, error, loading, refetch } = useQuery(AllReportsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-first',
        pollInterval: REFETCH_INTERVAL_MS,
    });

    useEffect(() => {
        const onFocus = () => {
            void refetch();
        };

        window.addEventListener('focus', onFocus);

        return () => window.removeEventListener('focus', onFocus);
    }, [refetch]);

    const [deleteMutation] = useMutation(DeleteReportDocument);
    const [resumeMutation] = useMutation(ResumeReportDocument);

    const reports = useMemo<ReportProgress[]>(() => data?.reports ?? [], [data?.reports]);

    const deleteReport = useCallback(
        async (report: ReportProgress): Promise<boolean> => {
            try {
                await deleteMutation({ variables: { reportId: report.id } });
                await refetch();

                return true;
            } catch (err) {
                Log.error('Failed to delete report:', err);
                toast.error('Failed to delete the report');

                return false;
            }
        },
        [deleteMutation, refetch],
    );

    // resumeReport re-queues a report's unwritten sections and relaunches the
    // run; the poll picks the fresh state up (no user-keyed live channel).
    const resumeReport = useCallback(
        async (report: ReportProgress, contentBudgetSeconds?: number): Promise<boolean> => {
            try {
                await resumeMutation({ variables: { contentBudgetSeconds, reportId: report.id } });

                return true;
            } catch (err) {
                Log.error('Failed to resume report:', err);
                toast.error('Failed to resume the report');

                return false;
            }
        },
        [resumeMutation],
    );

    return {
        deleteReport,
        error,
        loading,
        refetch,
        reports,
        resumeReport,
    };
}
