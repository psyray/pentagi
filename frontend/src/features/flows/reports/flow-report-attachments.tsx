import { Download, Paperclip, Plus, X } from 'lucide-react';
import { useState } from 'react';

import type { ReportSectionFragmentFragment } from '@/graphql/types';

import { Button } from '@/components/ui/button';
import { buildPathsQuery } from '@/features/resources/resources-utils';
import { baseUrl } from '@/models/api';

import type { ReportSectionEditor } from './use-flow-report-editor';

import FlowReportAttachmentPicker from './flow-report-attachment-picker';
import { formatByteCount } from './report-format';

interface FlowReportAttachmentsProps {
    attachments: readonly ReportAttachment[];
    canEdit: boolean;
    editor: ReportSectionEditor;
    flowId: null | string;
    sectionId: string;
}

type ReportAttachment = ReportSectionFragmentFragment['attachments'][number];

// The flow-files endpoint only serves cache-relative paths, so an absolute
// container path gets no direct download link (it still travels in the archive).
const attachmentHref = (flowId: null | string, path: string): null | string => {
    if (!flowId || path.startsWith('/')) {
        return null;
    }

    return `${baseUrl}/flows/${flowId}/files/download?${buildPathsQuery([path])}`;
};

function FlowReportAttachments({ attachments, canEdit, editor, flowId, sectionId }: FlowReportAttachmentsProps) {
    const [pickerOpen, setPickerOpen] = useState(false);

    if (attachments.length === 0 && !canEdit) {
        return null;
    }

    const remove = (path: string): void => {
        void editor.setAttachments(
            sectionId,
            attachments
                .filter((attachment) => attachment.path !== path)
                .map((attachment) => ({
                    bytes: attachment.bytes,
                    label: attachment.label,
                    path: attachment.path,
                })),
        );
    };

    return (
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <Paperclip className="text-muted-foreground size-3.5 shrink-0" />
            {attachments.map((attachment) => {
                const href = attachmentHref(flowId, attachment.path);

                return (
                    <span
                        className="bg-muted/60 inline-flex max-w-full items-center gap-1 rounded-md border px-2 py-0.5 text-xs"
                        key={attachment.id}
                        title={attachment.path}
                    >
                        {href ? (
                            <a
                                className="text-primary flex min-w-0 items-center gap-1 hover:underline"
                                href={href}
                            >
                                <Download className="size-3 shrink-0" />
                                <span className="truncate">{attachment.label}</span>
                            </a>
                        ) : (
                            <span className="min-w-0 truncate">{attachment.label}</span>
                        )}
                        <span className="text-muted-foreground shrink-0">{formatByteCount(attachment.bytes)}</span>
                        {canEdit && (
                            <button
                                aria-label={`Remove ${attachment.label}`}
                                className="text-muted-foreground hover:text-foreground shrink-0"
                                onClick={() => remove(attachment.path)}
                                title="Remove attachment"
                                type="button"
                            >
                                <X className="size-3" />
                            </button>
                        )}
                    </span>
                );
            })}
            {canEdit && (
                <Button
                    className="h-6 px-2 text-xs"
                    onClick={() => setPickerOpen(true)}
                    size="sm"
                    variant="ghost"
                >
                    <Plus className="size-3.5" />
                    Attach
                </Button>
            )}

            {pickerOpen && (
                <FlowReportAttachmentPicker
                    flowId={flowId}
                    onOpenChange={setPickerOpen}
                    onSave={(drafts) => editor.setAttachments(sectionId, drafts)}
                    selected={attachments}
                />
            )}
        </div>
    );
}

export default FlowReportAttachments;
