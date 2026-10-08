import { ChevronDown, Download, FileArchive, FileCode2, FileText, FileType2 } from 'lucide-react';

import { Button } from '@/components/ui/button';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Spinner } from '@/components/ui/spinner';

import type { ReportDownloadFormat } from './report-download';

interface FlowReportDownloadProps {
    canDownload: boolean;
    downloading: boolean;
    onDownload: (format: ReportDownloadFormat) => void;
}

// Menu mirrored on the enterprise edition: Markdown, PDF, Word (DOCX) and the
// archive. The archive is the document plus the files attached to its sections.
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
                <DropdownMenuLabel>Download</DropdownMenuLabel>
                <DropdownMenuItem onSelect={() => onDownload('markdown')}>
                    <FileText className="size-4" />
                    Markdown
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onDownload('pdf')}>
                    <FileType2 className="size-4" />
                    PDF
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onDownload('docx')}>
                    <FileCode2 className="size-4" />
                    Word (DOCX)
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onDownload('zip')}>
                    <FileArchive className="size-4" />
                    Archive (ZIP)
                    <span className="text-muted-foreground ml-auto pl-3 text-xs">with files</span>
                </DropdownMenuItem>
            </DropdownMenuContent>
        </DropdownMenu>
    );
}

export default FlowReportDownload;
