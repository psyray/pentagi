import { useApolloClient, useMutation, useQuery, useSubscription } from '@apollo/client/react';
import { useCallback, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { toast } from 'sonner';

import {
    DeleteReportDocument,
    FlowReportsDocument,
    ReportBrandingDocument,
    ReportRowDocument,
    ReportSectionsDocument,
    ReportSectionUpdatedDocument,
    ReportUpdatedDocument,
    ResumeReportDocument,
} from '@/graphql/types';
import { Log } from '@/lib/log';

import {
    downloadReportDocx,
    downloadReportMarkdown,
    downloadReportPdf,
    downloadReportZip,
    fetchReportDocument,
    type ReportDownloadFormat,
} from './report-download';
import { findReportById } from './report-outline';

/**
 * Live view of one report document. The report row comes from the flow's report
 * list (the same query the side tab uses) so the flow-scoped `reportUpdated`
 * frame lands in a cache field this page already watches; the sections come
 * from `reportSections(reportId)` and are refreshed by `reportSectionUpdated`.
 * Both writes are done by the Apollo subscription link.
 *
 * The flow id normally comes from the URL (`/flows/:flowId/reports/:reportId`).
 * On the cross-flow route (`/reports/:reportId`) it is read from the report row
 * first, then the same flow-scoped queries take over.
 */
export function useFlowReportDocument() {
    const { flowId: flowIdParam, reportId } = useParams<{ flowId?: string; reportId: string }>();
    const client = useApolloClient();
    const [downloading, setDownloading] = useState(false);
    const [deleteMutation] = useMutation(DeleteReportDocument);
    const [resumeMutation] = useMutation(ResumeReportDocument);

    // Cross-flow route only: the report row carries the owning flow, which the
    // flow-scoped list query and subscriptions need. Skipped when the URL has it.
    const rowQuery = useQuery(ReportRowDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-first',
        skip: !reportId || !!flowIdParam,
        variables: { reportId: reportId ?? '' },
    });

    const flowId = flowIdParam ?? rowQuery.data?.report?.flowId ?? '';

    const reportsQuery = useQuery(FlowReportsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-first',
        skip: !flowId,
        variables: { flowId },
    });

    const sectionsQuery = useQuery(ReportSectionsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-first',
        skip: !reportId,
        variables: { reportId: reportId ?? '' },
    });

    // The exports carry the branding; the panel reads the same cached query.
    const brandingQuery = useQuery(ReportBrandingDocument, {
        skip: !reportId,
        variables: { reportId: reportId ?? '' },
    });

    const subscriptionVariables = useMemo(() => ({ flowId }), [flowId]);
    const subscriptionSkip = !flowId;

    useSubscription(ReportUpdatedDocument, { skip: subscriptionSkip, variables: subscriptionVariables });
    useSubscription(ReportSectionUpdatedDocument, { skip: subscriptionSkip, variables: subscriptionVariables });

    const report = useMemo(
        () => findReportById(reportsQuery.data?.reports ?? [], reportId),
        [reportId, reportsQuery.data?.reports],
    );
    const sections = sectionsQuery.data?.reportSections ?? [];
    const branding = brandingQuery.data?.reportBranding ?? null;

    const download = useCallback(
        async (format: ReportDownloadFormat): Promise<void> => {
            if (!reportId || !report) {
                return;
            }

            setDownloading(true);

            try {
                if (format === 'zip') {
                    await downloadReportZip(report);

                    return;
                }

                const content = await fetchReportDocument(client, reportId);

                if (!content.trim()) {
                    toast.error('The report document is empty');

                    return;
                }

                if (format === 'pdf') {
                    await downloadReportPdf(report, content, branding);
                } else if (format === 'docx') {
                    await downloadReportDocx(report, content, branding);
                } else {
                    downloadReportMarkdown(report, content);
                }
            } catch (error) {
                Log.error('Failed to download the report document:', error);
                toast.error(error instanceof Error ? error.message : 'Failed to download the report');
            } finally {
                setDownloading(false);
            }
        },
        [branding, client, report, reportId],
    );

    const remove = useCallback(async (): Promise<boolean> => {
        if (!reportId) {
            return false;
        }

        try {
            await deleteMutation({ variables: { reportId } });

            return true;
        } catch (error) {
            Log.error('Failed to delete the report:', error);
            toast.error('Failed to delete the report');

            return false;
        }
    }, [deleteMutation, reportId]);

    const resume = useCallback(
        async (contentBudgetSeconds?: number): Promise<boolean> => {
            if (!reportId) {
                return false;
            }

            try {
                await resumeMutation({ variables: { contentBudgetSeconds, reportId } });

                return true;
            } catch (error) {
                Log.error('Failed to resume the report:', error);
                toast.error('Failed to resume the report');

                return false;
            }
        },
        [reportId, resumeMutation],
    );

    return {
        branding,
        download,
        downloading,
        flowId,
        loading:
            (!flowIdParam && rowQuery.loading && !rowQuery.data?.report) ||
            (!!flowId && reportsQuery.loading && !report) ||
            (sectionsQuery.loading && sections.length === 0),
        remove,
        report,
        reportId: reportId ?? '',
        resume,
        sections,
    };
}
