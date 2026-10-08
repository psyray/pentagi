import { useApolloClient, useQuery, useSubscription } from '@apollo/client/react';
import { useCallback, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { toast } from 'sonner';

import {
    FlowReportsDocument,
    ReportSectionsDocument,
    ReportSectionUpdatedDocument,
    ReportUpdatedDocument,
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
 */
export function useFlowReportDocument() {
    const { flowId, reportId } = useParams<{ flowId: string; reportId: string }>();
    const client = useApolloClient();
    const [downloading, setDownloading] = useState(false);

    const reportsQuery = useQuery(FlowReportsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-first',
        skip: !flowId,
        variables: { flowId: flowId ?? '' },
    });

    const sectionsQuery = useQuery(ReportSectionsDocument, {
        fetchPolicy: 'cache-and-network',
        nextFetchPolicy: 'cache-first',
        skip: !reportId,
        variables: { reportId: reportId ?? '' },
    });

    const subscriptionVariables = useMemo(() => ({ flowId: flowId ?? '' }), [flowId]);
    const subscriptionSkip = !flowId;

    useSubscription(ReportUpdatedDocument, { skip: subscriptionSkip, variables: subscriptionVariables });
    useSubscription(ReportSectionUpdatedDocument, { skip: subscriptionSkip, variables: subscriptionVariables });

    const report = useMemo(
        () => findReportById(reportsQuery.data?.reports ?? [], reportId),
        [reportId, reportsQuery.data?.reports],
    );
    const sections = sectionsQuery.data?.reportSections ?? [];

    const download = useCallback(
        async (format: ReportDownloadFormat): Promise<void> => {
            if (!flowId || !reportId || !report) {
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
                    await downloadReportPdf(report, content);
                } else if (format === 'docx') {
                    await downloadReportDocx(report, content);
                } else {
                    downloadReportMarkdown(report, content);
                }
            } catch (error) {
                Log.error('Failed to download the report document:', error);
                toast.error('Failed to download the report');
            } finally {
                setDownloading(false);
            }
        },
        [client, flowId, report, reportId],
    );

    return {
        download,
        downloading,
        loading: (reportsQuery.loading && !report) || (sectionsQuery.loading && sections.length === 0),
        report,
        reportId: reportId ?? '',
        sections,
    };
}
