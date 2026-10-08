import { ChevronDown, Download, FileText } from 'lucide-react';

import { Button } from '@/components/ui/button';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Spinner } from '@/components/ui/spinner';

import type { ReportDownloadFormat } from './report-download';

interface FlowReportDownloadProps {
    canDownload: boolean;
    downloading: boolean;
    onDownload: (format: ReportDownloadFormat) => void;
}

// Menu mirror of the enterprise edition: Markdown and PDF in v1; Word (DOCX)
// and Archive (ZIP) land with the finishing phase (plan § 6.6 / § 6.9).
function FlowReportDownload({ canDownload, downloading, onDownload }: FlowReportDownloadProps) {
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button
                    disabled={!canDownload || downloading}
                    size="sm"
                    title={canDownload ? 'Download report' : 'The report has not finished generating yet'}
                    variant="outline"
                >
                    {downloading ? <Spinner className="size-4" /> : <Download className="size-4" />}
                    Download
                    <ChevronDown className="size-4" />
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => onDownload('markdown')}>
                    <FileText className="size-4" />
                    Markdown
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onDownload('pdf')}>
                    <FileText className="size-4" />
                    PDF
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    );
}

export default FlowReportDownload;
