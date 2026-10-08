import type { ApolloClient } from '@apollo/client';

import { ReportDocumentDocument, type ReportFragmentFragment } from '@/graphql/types';
import { api } from '@/lib/axios';
import { downloadTextFile, generatePDFFromMarkdown } from '@/lib/report';

export type ReportDownloadFormat = 'docx' | 'markdown' | 'pdf' | 'zip';

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

const triggerBlobDownload = (blob: Blob, fileName: string): void => {
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');

    link.href = url;
    link.download = fileName;
    link.style.display = 'none';

    document.body.append(link);
    link.click();
    link.remove();

    URL.revokeObjectURL(url);
};

export const downloadReportMarkdown = (report: ReportFragmentFragment, content: string): void => {
    downloadTextFile(content, `${reportFileName(report)}.md`, 'text/markdown; charset=UTF-8');
};

export const downloadReportPdf = async (report: ReportFragmentFragment, content: string): Promise<void> => {
    // The AI report opens chapters and findings on fresh pages, like the DOCX.
    await generatePDFFromMarkdown(content, reportFileName(report), { paginateSections: true });
};

// The Word builder is heavy (docx) and only fetched when the user actually
// picks the format, not on every page that imports this module.
export const downloadReportDocx = async (report: ReportFragmentFragment, content: string): Promise<void> => {
    const { markdownToDocxBlob } = await import('./report-docx');

    triggerBlobDownload(await markdownToDocxBlob(content), `${reportFileName(report)}.docx`);
};

// The archive is assembled server-side: the document plus the container files
// and screenshots its sections cited (plan § 6.10). Streaming the ZIP from the
// server keeps the container reads on the backend.
export const downloadReportZip = async (report: ReportFragmentFragment): Promise<void> => {
    const blob = (await api.get<Blob>(`/flows/${report.flowId}/reports/${report.id}/archive`, {
        responseType: 'blob',
    })) as unknown as Blob;

    triggerBlobDownload(blob, `${reportFileName(report)}_annexes.zip`);
};

// Fetches the server-assembled document (numbered sections, resolved
// placeholders, computed composed sections) — the same markdown the reader
// previews and every export format walks.
export const fetchReportDocument = async (client: ApolloClient, reportId: string): Promise<string> => {
    const response = await client.query({
        fetchPolicy: 'network-only',
        query: ReportDocumentDocument,
        variables: { reportId },
    });

    return response.data?.reportDocument ?? '';
};
