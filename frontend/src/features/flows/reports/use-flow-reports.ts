import { useApolloClient, useMutation, useQuery, useSubscription } from '@apollo/client/react';
import { useCallback, useMemo } from 'react';
import { toast } from 'sonner';

import {
    CreateReportDocument,
    DeleteReportDocument,
    FlowReportsDocument,
    ReportCreatedDocument,
    ReportDeletedDocument,
    ReportUpdatedDocument,
    ReportVisionDocument,
    ResumeReportDocument,
} from '@/graphql/types';
import { type ReportFragmentFragment } from '@/graphql/types';
import { Log } from '@/lib/log';
import { useFlow } from '@/providers/flow-provider';

export interface CreateReportInput {
    additionalInstructions?: null | string;
    contentBudgetSeconds?: null | number;
    controlBudgetSeconds?: null | number;
    language?: null | string;
    providerName?: null | string;
    templateKey?: null | string;
    title?: null | string;
}

export type ReportProgress = ReportFragmentFragment;

export interface ReportVisionProbe {
    error?: null | string;
    vision: boolean;
}

// sortReports keeps the list stable as live events land (newest first).
const sortReports = (reports: readonly ReportProgress[]): ReportProgress[] =>
    [...reports].sort((left, right) => {
        const leftTime = new Date(left.createdAt).getTime();
        const rightTime = new Date(right.createdAt).getTime();

        if (leftTime !== rightTime) {
            return rightTime - leftTime;
        }

        const leftId = Number.parseInt(left.id, 10) || 0;
        const rightId = Number.parseInt(right.id, 10) || 0;

        return rightId - leftId;
    });

/**
 * Live view of the flow's reports. One query at mount; the three report
 * subscriptions write straight into the Apollo cache, so the component tree
 * re-renders through the same query result the list reads.
 */
export function useFlowReports() {
    const { flowId } = useFlow();
    const client = useApolloClient();

    const { data, loading, refetch } = useQuery(FlowReportsDocument, {
        skip: !flowId,
        variables: { flowId: flowId ?? '' },
    });

    const reports = useMemo(() => data?.reports ?? [], [data?.reports]);

    const mergeReport = useCallback(
        (report: ReportProgress) => {
            if (!flowId) {
                return;
            }

            const current = client.cache.readQuery({
                query: FlowReportsDocument,
                variables: { flowId },
            });

            client.cache.writeQuery({
                data: {
                    reports: sortReports([report, ...(current?.reports ?? []).filter((row) => row.id !== report.id)]),
                },
                query: FlowReportsDocument,
                variables: { flowId },
            });
        },
        [client, flowId],
    );

    const dropReport = useCallback(
        (report: ReportProgress) => {
            if (!flowId) {
                return;
            }

            const current = client.cache.readQuery({
                query: FlowReportsDocument,
                variables: { flowId },
            });

            client.cache.writeQuery({
                data: {
                    reports: (current?.reports ?? []).filter((row) => row.id !== report.id),
                },
                query: FlowReportsDocument,
                variables: { flowId },
            });
        },
        [client, flowId],
    );

    const subscriptionVariables = useMemo(() => ({ flowId: flowId ?? '' }), [flowId]);
    const subscriptionSkip = !flowId;

    useSubscription(ReportCreatedDocument, {
        onData: ({ data: event }) => {
            const report = event.data?.reportCreated;

            if (report) {
                mergeReport(report);
            }
        },
        skip: subscriptionSkip,
        variables: subscriptionVariables,
    });

    useSubscription(ReportUpdatedDocument, {
        onData: ({ data: event }) => {
            const report = event.data?.reportUpdated;

            if (report) {
                mergeReport(report);
            }
        },
        skip: subscriptionSkip,
        variables: subscriptionVariables,
    });

    useSubscription(ReportDeletedDocument, {
        onData: ({ data: event }) => {
            const report = event.data?.reportDeleted;

            if (report) {
                dropReport(report);
            }
        },
        skip: subscriptionSkip,
        variables: subscriptionVariables,
    });

    const [createMutation] = useMutation(CreateReportDocument);
    const [deleteMutation] = useMutation(DeleteReportDocument);
    const [resumeMutation] = useMutation(ResumeReportDocument);

    // Probed before creating a report: warns when the chosen model cannot read
    // images, so screenshots would not be analysed (plan § 6.10).
    const checkVision = useCallback(
        async (providerName: null | string): Promise<ReportVisionProbe> => {
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

    const createReport = useCallback(
        async (input: CreateReportInput): Promise<null | ReportProgress> => {
            if (!flowId) {
                return null;
            }

            try {
                const response = await createMutation({ variables: { flowId, input } });
                const report = response.data?.createReport as ReportProgress | undefined;

                if (report) {
                    mergeReport(report);

                    return report;
                }
            } catch (error) {
                Log.error('Failed to create report:', error);
            }

            toast.error('Failed to create the report');

            return null;
        },
        [createMutation, flowId, mergeReport],
    );

    const deleteReport = useCallback(
        async (report: ReportProgress): Promise<boolean> => {
            try {
                await deleteMutation({ variables: { reportId: report.id } });
                dropReport(report);

                return true;
            } catch (error) {
                Log.error('Failed to delete report:', error);
                toast.error('Failed to delete the report');

                return false;
            }
        },
        [deleteMutation, dropReport],
    );

    // resumeReport re-queues a report's unwritten sections and relaunches the
    // run; contentBudgetSeconds grants fresh content time (plan backlog v2, L1).
    const resumeReport = useCallback(
        async (report: ReportProgress, contentBudgetSeconds?: number): Promise<boolean> => {
            try {
                const response = await resumeMutation({
                    variables: { contentBudgetSeconds, reportId: report.id },
                });
                const updated = response.data?.resumeReport as ReportProgress | undefined;

                if (updated) {
                    mergeReport(updated);
                }

                return true;
            } catch (error) {
                Log.error('Failed to resume report:', error);
                toast.error('Failed to resume the report');

                return false;
            }
        },
        [mergeReport, resumeMutation],
    );

    return {
        checkVision,
        createReport,
        deleteReport,
        loading: loading,
        refetch,
        reports,
        resumeReport,
    };
}
