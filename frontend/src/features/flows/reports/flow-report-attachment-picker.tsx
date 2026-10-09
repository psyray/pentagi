import { useMemo, useState } from 'react';

import type { FileNode } from '@/components/shared/file-manager';
import type { ReportSectionFragmentFragment } from '@/graphql/types';

import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Spinner } from '@/components/ui/spinner';
import { useFlowFilesData } from '@/features/flows/files/use-flow-files-data';
import { cn } from '@/lib/utils';

import type { ReportAttachmentDraft } from './use-flow-report-editor';

import { formatByteCount } from './report-format';

interface FlowReportAttachmentPickerProps {
    flowId: null | string;
    onOpenChange: (open: boolean) => void;
    onSave: (attachments: ReportAttachmentDraft[]) => Promise<boolean> | void;
    selected: readonly ReportAttachment[];
}

type ReportAttachment = ReportSectionFragmentFragment['attachments'][number];

const fileLabel = (path: string): string => path.split('/').pop() || path;

// The picker is mounted only while the dialog is open, so its draft seeds the
// section's current attachments at mount and is dropped on close. The flow's
// cached files are toggled in and out, and a free-text row accepts any other
// path. Saving replaces the section's attachments wholesale (backlog v2, L6).
const seedDraft = (selected: readonly ReportAttachment[]): Record<string, ReportAttachmentDraft> => {
    const next: Record<string, ReportAttachmentDraft> = {};

    for (const attachment of selected) {
        next[attachment.path] = {
            bytes: attachment.bytes,
            label: attachment.label || fileLabel(attachment.path),
            path: attachment.path,
        };
    }

    return next;
};

function FlowReportAttachmentPicker({ flowId, onOpenChange, onSave, selected }: FlowReportAttachmentPickerProps) {
    const { fileNodes, isInitialLoading } = useFlowFilesData({ flowId });
    const [draft, setDraft] = useState<Record<string, ReportAttachmentDraft>>(() => seedDraft(selected));
    const [manualPath, setManualPath] = useState('');
    const [saving, setSaving] = useState(false);

    const files = useMemo(() => fileNodes.filter((node) => !node.isDir), [fileNodes]);
    const draftPaths = useMemo(() => new Set(Object.keys(draft)), [draft]);

    const toggle = (file: FileNode): void => {
        setDraft((current) => {
            const next = { ...current };

            if (next[file.path]) {
                delete next[file.path];

                return next;
            }

            next[file.path] = {
                bytes: file.size ?? 0,
                label: file.name || fileLabel(file.path),
                path: file.path,
            };

            return next;
        });
    };

    const addManual = (): void => {
        const path = manualPath.trim();

        if (path === '') {
            return;
        }

        setDraft((current) => ({
            ...current,
            [path]: { bytes: 0, label: fileLabel(path), path },
        }));
        setManualPath('');
    };

    const handleSave = async (): Promise<void> => {
        setSaving(true);

        try {
            if ((await onSave(Object.values(draft))) === false) {
                return;
            }

            onOpenChange(false);
        } finally {
            setSaving(false);
        }
    };

    return (
        <Dialog
            onOpenChange={onOpenChange}
            open
        >
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>Attach files</DialogTitle>
                    <DialogDescription>
                        Pick files from the flow workspace to bundle in the export archive. The text exports only list
                        them.
                    </DialogDescription>
                </DialogHeader>

                <div className="flex flex-col gap-3">
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="report-attachment-input">Add a path</Label>
                        <div className="flex gap-2">
                            <Input
                                id="report-attachment-input"
                                onChange={(event) => setManualPath(event.target.value)}
                                onKeyDown={(event) => {
                                    if (event.key === 'Enter') {
                                        event.preventDefault();
                                        addManual();
                                    }
                                }}
                                placeholder="uploads/report.pdf or /work/notes.txt"
                                value={manualPath}
                            />
                            <Button
                                onClick={addManual}
                                type="button"
                                variant="secondary"
                            >
                                Add
                            </Button>
                        </div>
                    </div>

                    <div className="flex max-h-72 min-h-24 flex-col overflow-hidden rounded-md border">
                        {isInitialLoading ? (
                            <div className="flex flex-1 items-center justify-center p-4">
                                <Spinner className="size-4" />
                            </div>
                        ) : files.length === 0 ? (
                            <p className="text-muted-foreground p-3 text-sm">
                                No file in this flow&apos;s workspace yet.
                            </p>
                        ) : (
                            <ScrollArea className="[&_[data-slot=scroll-area-viewport]>div]:block!">
                                <ul className="flex flex-col">
                                    {files.map((file) => {
                                        const checked = draftPaths.has(file.path);

                                        return (
                                            <li key={file.path}>
                                                <label
                                                    className={cn(
                                                        'hover:bg-accent flex cursor-pointer items-center gap-2 px-3 py-2',
                                                        checked && 'bg-accent/60',
                                                    )}
                                                >
                                                    <Checkbox
                                                        checked={checked}
                                                        onCheckedChange={() => toggle(file)}
                                                    />
                                                    <span
                                                        className="min-w-0 flex-1 truncate text-sm"
                                                        title={file.path}
                                                    >
                                                        {file.path}
                                                    </span>
                                                    <span className="text-muted-foreground shrink-0 text-xs">
                                                        {formatByteCount(file.size ?? 0)}
                                                    </span>
                                                </label>
                                            </li>
                                        );
                                    })}
                                </ul>
                            </ScrollArea>
                        )}
                    </div>

                    {draftPaths.size > 0 && (
                        <p className="text-muted-foreground text-xs">
                            {draftPaths.size} file{draftPaths.size > 1 ? 's' : ''} selected
                        </p>
                    )}
                </div>

                <DialogFooter>
                    <Button
                        onClick={() => onOpenChange(false)}
                        type="button"
                        variant="outline"
                    >
                        Cancel
                    </Button>
                    <Button
                        disabled={saving}
                        onClick={() => {
                            void handleSave();
                        }}
                        type="button"
                    >
                        {saving && <Spinner variant="circle" />}
                        Save
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

export default FlowReportAttachmentPicker;
