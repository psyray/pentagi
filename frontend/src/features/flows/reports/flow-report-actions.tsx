import { Ellipsis, FileArchive, FileCode2, FileText, FileType2, Trash2 } from 'lucide-react';

import { Button } from '@/components/ui/button';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Spinner } from '@/components/ui/spinner';

import type { ReportDownloadFormat } from './report-download';

interface FlowReportActionsProps {
    canDelete: boolean;
    canDownload: boolean;
    downloading: boolean;
    onDelete: () => void;
    onDownload: (format: ReportDownloadFormat) => void;
}

// The report header's overflow menu, mirroring the enterprise edition: the four
// export formats and the delete action behind one "..." trigger (backlog v2, L7).
function FlowReportActions({ canDelete, canDownload, downloading, onDelete, onDownload }: FlowReportActionsProps) {
    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button
                    aria-label="Report actions"
                    size="icon"
                    title="Report actions"
                    variant="outline"
                >
                    {downloading ? <Spinner className="size-4" /> : <Ellipsis className="size-4" />}
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
                <DropdownMenuLabel>Export</DropdownMenuLabel>
                <DropdownMenuItem
                    disabled={!canDownload}
                    onSelect={() => onDownload('markdown')}
                >
                    <FileText className="size-4" />
                    Markdown
                </DropdownMenuItem>
                <DropdownMenuItem
                    disabled={!canDownload}
                    onSelect={() => onDownload('pdf')}
                >
                    <FileType2 className="size-4" />
                    PDF
                </DropdownMenuItem>
                <DropdownMenuItem
                    disabled={!canDownload}
                    onSelect={() => onDownload('docx')}
                >
                    <FileCode2 className="size-4" />
                    Word (DOCX)
                </DropdownMenuItem>
                <DropdownMenuItem
                    disabled={!canDownload}
                    onSelect={() => onDownload('zip')}
                >
                    <FileArchive className="size-4" />
                    Archive (ZIP)
                    <span className="text-muted-foreground ml-auto pl-3 text-xs">with files</span>
                </DropdownMenuItem>
                {canDelete && (
                    <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                            onSelect={onDelete}
                            variant="destructive"
                        >
                            <Trash2 className="size-4" />
                            Delete report
                        </DropdownMenuItem>
                    </>
                )}
            </DropdownMenuContent>
        </DropdownMenu>
    );
}

export default FlowReportActions;
