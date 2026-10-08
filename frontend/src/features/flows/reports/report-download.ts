import type { ApolloClient } from '@apollo/client';

import type { PdfBranding } from '@/lib/report';

import {
    type ReportBrandingFragmentFragment,
    ReportDocumentDocument,
    type ReportFragmentFragment,
} from '@/graphql/types';
import { api } from '@/lib/axios';
import { Log } from '@/lib/log';
import { arrayBufferToDataUrl, downloadTextFile, generatePDFFromMarkdown, scaleImage } from '@/lib/report';

import type { DocxBranding } from './report-docx';

import { BRANDING_LOGO_MAX_WIDTH, measureImage, reportBrandingLogoPath } from './report-branding';

export type ReportDownloadFormat = 'docx' | 'markdown' | 'pdf' | 'zip';

type ReportBranding = null | ReportBrandingFragmentFragment;

// The logo bytes are fetched from the API (cookie-authenticated), never carried
// through GraphQL; the renderers take a data URL. A logo that cannot be read
// degrades to a logo-less export rather than failing the whole download.
const loadBrandingLogo = async (
    report: ReportFragmentFragment,
    branding: ReportBranding,
): Promise<null | { dataUrl: string; mediaType: string }> => {
    if (!branding?.hasLogo) {
        return null;
    }

    const mediaType = branding.logoMediaType ?? 'image/png';

    if (mediaType !== 'image/jpeg' && mediaType !== 'image/png') {
        return null;
    }

    try {
        const buffer = (await api.get(reportBrandingLogoPath(report.flowId, report.id), {
            responseType: 'arraybuffer',
        })) as unknown as ArrayBuffer;

        return { dataUrl: arrayBufferToDataUrl(buffer, mediaType), mediaType };
    } catch (error) {
        Log.error('The report logo could not be fetched; exporting without it:', error);

        return null;
    }
};

const buildDocxBranding = async (
    report: ReportFragmentFragment,
    branding: ReportBranding,
): Promise<DocxBranding | undefined> => {
    if (!branding) {
        return undefined;
    }

    let logo: DocxBranding['logo'] = null;
    const loaded = await loadBrandingLogo(report, branding);

    if (loaded) {
        try {
            const size = scaleImage(await measureImage(loaded.dataUrl), BRANDING_LOGO_MAX_WIDTH);

            if (size.width > 0 && size.height > 0) {
                logo = { dataUrl: loaded.dataUrl, height: size.height, width: size.width };
            }
        } catch {
            // An image the browser cannot decode is dropped rather than failing
            // the export.
            logo = null;
        }
    }

    return { footer: branding.resolvedFooter, header: branding.resolvedHeader, logo };
};

const buildPdfBranding = async (
    report: ReportFragmentFragment,
    branding: ReportBranding,
): Promise<PdfBranding | undefined> => {
    if (!branding) {
        return undefined;
    }

    let logo: PdfBranding['logo'] = null;
    const loaded = await loadBrandingLogo(report, branding);

    if (loaded) {
        logo = { dataUrl: loaded.dataUrl };

        // Sizing the logo here keeps react-pdf from having to measure it, and
        // falls back to a width-only image when the browser cannot decode it.
        try {
            const size = scaleImage(await measureImage(loaded.dataUrl), BRANDING_LOGO_MAX_WIDTH);

            if (size.width > 0 && size.height > 0) {
                logo = { dataUrl: loaded.dataUrl, height: size.height, width: size.width };
            }
        } catch {
            logo = { dataUrl: loaded.dataUrl };
        }
    }

    return { footer: branding.resolvedFooter, header: branding.resolvedHeader, logo };
};

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

export const downloadReportPdf = async (
    report: ReportFragmentFragment,
    content: string,
    branding?: ReportBranding,
): Promise<void> => {
    // The AI report opens chapters and findings on fresh pages, like the DOCX.
    await generatePDFFromMarkdown(content, reportFileName(report), {
        branding: await buildPdfBranding(report, branding ?? null),
        paginateSections: true,
    });
};

// The Word builder is heavy (docx) and only fetched when the user actually
// picks the format, not on every page that imports this module.
export const downloadReportDocx = async (
    report: ReportFragmentFragment,
    content: string,
    branding?: ReportBranding,
): Promise<void> => {
    const { markdownToDocxBlob } = await import('./report-docx');

    triggerBlobDownload(
        await markdownToDocxBlob(content, await buildDocxBranding(report, branding ?? null)),
        `${reportFileName(report)}.docx`,
    );
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
