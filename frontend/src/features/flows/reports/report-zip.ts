import JSZip from 'jszip';

export interface ReportArchiveFile {
    content: string;
    name: string;
}

/**
 * Bundles the assembled document with the files attached to its sections.
 * Attachments are a v2 feature (plan § 6.9), so the archive carries the
 * document alone until they land; the signature is already shaped for them.
 */
export const buildReportArchiveBlob = async (
    documentName: string,
    markdown: string,
    files: readonly ReportArchiveFile[] = [],
): Promise<Blob> => {
    const zip = new JSZip();

    zip.file(`${documentName}.md`, markdown);

    for (const file of files) {
        zip.file(file.name, file.content);
    }

    return zip.generateAsync({ type: 'blob' });
};
