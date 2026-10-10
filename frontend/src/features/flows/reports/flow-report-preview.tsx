import { useApolloClient } from '@apollo/client/react';
import { ChevronLeft, ChevronRight, Minus, Plus } from 'lucide-react';
import workerSrc from 'pdfjs-dist/build/pdf.worker.min.mjs?url';
import 'react-pdf/dist/Page/AnnotationLayer.css';
import 'react-pdf/dist/Page/TextLayer.css';
import { useCallback, useEffect, useState } from 'react';
import { Document, Page, pdfjs } from 'react-pdf';

import type { ReportBrandingFragmentFragment, ReportFragmentFragment } from '@/graphql/types';

import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { Log } from '@/lib/log';

import { buildReportPdfBlob, fetchReportDocument } from './report-download';

// PDF.js renders off the main thread: point it at the worker Vite emitted.
pdfjs.GlobalWorkerOptions.workerSrc = workerSrc;

// Page pixel widths offered by the zoom buttons (A4 proportions).
const PAGE_WIDTHS = [560, 720, 900] as const;
const DEFAULT_WIDTH_INDEX = 1;

// One blob per (report revision + branding) so toggling the preview repeatedly
// does not rebuild the PDF; the reportUpdated frame bumps updatedAt on a change.
const previewCache = new Map<string, Blob>();

const cacheKey = (
    report: ReportFragmentFragment,
    branding: null | ReportBrandingFragmentFragment,
    content: string,
): string =>
    [
        report.id,
        report.updatedAt,
        content.length,
        branding?.hasLogo ? 'logo' : 'nologo',
        branding?.logoMediaType ?? '',
        branding?.resolvedHeader ?? '',
        branding?.resolvedFooter ?? '',
        branding?.resolvedSubtitle ?? '',
    ].join(':');

interface FlowReportPreviewProps {
    branding: null | ReportBrandingFragmentFragment;
    report: ReportFragmentFragment;
    reportId: string;
}

/**
 * Print preview of a finished report: the same PDF the export produces, rendered
 * page by page with PDF.js (backlog v2, L7). It builds the blob client-side on
 * demand behind a toggle, so the page count is the export's page count.
 */
export function FlowReportPreview({ branding, report, reportId }: FlowReportPreviewProps) {
    const client = useApolloClient();
    const [blob, setBlob] = useState<Blob | null>(null);
    const [error, setError] = useState<null | string>(null);
    const [numPages, setNumPages] = useState(0);
    const [page, setPage] = useState(1);
    const [widthIndex, setWidthIndex] = useState(DEFAULT_WIDTH_INDEX);

    // Derived, not stored: the repo forbids a synchronous setState in an effect.
    const loading = blob === null && error === null;

    useEffect(() => {
        let cancelled = false;

        const build = async () => {
            const content = await fetchReportDocument(client, reportId);

            if (!content.trim()) {
                throw new Error('The report document is empty.');
            }

            const key = cacheKey(report, branding, content);
            const cached = previewCache.get(key);

            if (cached) {
                return cached;
            }

            const built = await buildReportPdfBlob(report, content, branding);

            previewCache.set(key, built);

            return built;
        };

        void build()
            .then((built) => {
                if (!cancelled) {
                    setError(null);
                    setBlob(built);
                }
            })
            .catch((buildError: unknown) => {
                if (!cancelled) {
                    Log.error('Failed to build the report preview:', buildError);
                    setBlob(null);
                    setError(buildError instanceof Error ? buildError.message : 'The preview could not be built.');
                }
            });

        return () => {
            cancelled = true;
        };
    }, [branding, client, report, reportId]);

    const zoomOut = useCallback(() => setWidthIndex((index) => Math.max(0, index - 1)), []);
    const zoomIn = useCallback(() => setWidthIndex((index) => Math.min(PAGE_WIDTHS.length - 1, index + 1)), []);

    return (
        <div className="flex h-full min-h-0 flex-col">
            <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
                <span className="text-muted-foreground text-xs tabular-nums">
                    {numPages > 0 ? `Page ${page} / ${numPages}` : loading ? 'Building preview…' : ''}
                </span>
                <div className="flex items-center gap-1">
                    <Button
                        aria-label="Previous page"
                        disabled={page <= 1}
                        onClick={() => setPage((current) => Math.max(1, current - 1))}
                        size="icon"
                        variant="ghost"
                    >
                        <ChevronLeft className="size-4" />
                    </Button>
                    <Button
                        aria-label="Next page"
                        disabled={numPages === 0 || page >= numPages}
                        onClick={() => setPage((current) => Math.min(numPages, current + 1))}
                        size="icon"
                        variant="ghost"
                    >
                        <ChevronRight className="size-4" />
                    </Button>
                    <Button
                        aria-label="Zoom out"
                        disabled={widthIndex === 0}
                        onClick={zoomOut}
                        size="icon"
                        variant="ghost"
                    >
                        <Minus className="size-4" />
                    </Button>
                    <Button
                        aria-label="Zoom in"
                        disabled={widthIndex === PAGE_WIDTHS.length - 1}
                        onClick={zoomIn}
                        size="icon"
                        variant="ghost"
                    >
                        <Plus className="size-4" />
                    </Button>
                </div>
            </div>

            <div className="bg-muted/40 min-h-0 flex-1 overflow-auto p-4">
                <div className="flex justify-center">
                    {loading ? (
                        <div className="flex items-center gap-2 py-10 text-sm">
                            <Spinner className="size-4" />
                            <span className="text-muted-foreground">Building the preview…</span>
                        </div>
                    ) : error ? (
                        <div className="text-muted-foreground max-w-md py-10 text-center text-sm">{error}</div>
                    ) : (
                        blob && (
                            <Document
                                file={blob}
                                onLoadError={(loadError) => {
                                    Log.error('The PDF preview could not be loaded:', loadError);
                                    setError('The preview could not be loaded.');
                                }}
                                onLoadSuccess={({ numPages: loaded }) => {
                                    setNumPages(loaded);
                                    setPage(1);
                                }}
                            >
                                <Page
                                    pageNumber={page}
                                    renderAnnotationLayer={false}
                                    width={PAGE_WIDTHS[widthIndex]}
                                />
                            </Document>
                        )
                    )}
                </div>
            </div>
        </div>
    );
}

export default FlowReportPreview;
