import type { ApolloClient } from '@apollo/client';

import { ReportDocumentDocument, type ReportFragmentFragment } from '@/graphql/types';
import { downloadTextFile, generatePDFFromMarkdown } from '@/lib/report';

export type ReportDownloadFormat = 'markdown' | 'pdf';

// Mirrors the legacy report naming so a user who exports both reads them in the
// same folder without thinking.
export const reportFileName = (report: ReportFragmentFragment): string => {
    const raw = report.title || report.templateLabel || `report-${report.id}`;

    const slug = raw
        .replaceAll(/[^\w\s.-]/g, '_')
        .replaceAll(/[\s\u2000-\u200B]+/g, '_')
        .toLowerCase()
        .slice(0, 150)
        .replace(/^_+/, '')
        .replace(/_+$/, '');

    return `report_${report.id}_${slug || 'document'}`;
};

export const downloadReportMarkdown = (report: ReportFragmentFragment, content: string): void => {
    downloadTextFile(content, `${reportFileName(report)}.md`, 'text/markdown; charset=UTF-8');
};

export const downloadReportPdf = async (report: ReportFragmentFragment, content: string): Promise<void> => {
    await generatePDFFromMarkdown(content, reportFileName(report));
};

// Fetches the server-assembled document (numbered sections, resolved
// placeholders, computed composed sections) — the same markdown the reader
// previews and both export formats walk.
export const fetchReportDocument = async (client: ApolloClient, reportId: string): Promise<string> => {
    const response = await client.query({
        fetchPolicy: 'network-only',
        query: ReportDocumentDocument,
        variables: { reportId },
    });

    return response.data?.reportDocument ?? '';
};
