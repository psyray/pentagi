import { ArrowDown, ArrowUp, Eye, EyeOff, Pencil, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';

import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Spinner } from '@/components/ui/spinner';
import { type ReportSectionFragmentFragment } from '@/graphql/types';
import { cn } from '@/lib/utils';

import type { SiblingMoveBoundaries } from './report-section-edit';
import type { ReportSectionEditor } from './use-flow-report-editor';

interface FlowReportAddSectionDialogProps {
    onOpenChange: (open: boolean) => void;
    onSubmit: (title: string) => Promise<boolean>;
    open: boolean;
    placement: string;
    saving: boolean;
}

interface FlowReportSectionActionsProps {
    boundaries: SiblingMoveBoundaries;
    busy: boolean;
    editor: ReportSectionEditor;
    onEdit: () => void;
    section: ReportSectionFragmentFragment;
}

function FlowReportAddSectionDialog({
    onOpenChange,
    onSubmit,
    open,
    placement,
    saving,
}: FlowReportAddSectionDialogProps) {
    const [title, setTitle] = useState('');
    const [submitting, setSubmitting] = useState(false);

    const handleSubmit = async () => {
        if (!title.trim() || submitting) {
            return;
        }

        setSubmitting(true);

        try {
            if (await onSubmit(title.trim())) {
                setTitle('');
                onOpenChange(false);
            }
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <Dialog
            onOpenChange={(next) => {
                if (!next) {
                    setTitle('');
                }

                onOpenChange(next);
            }}
            open={open}
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>Add a section</DialogTitle>
                    <DialogDescription>
                        The new section is created {placement}, empty and ready to edit.
                    </DialogDescription>
                </DialogHeader>
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="report-add-section-title">Title</Label>
                    <Input
                        autoFocus
                        id="report-add-section-title"
                        onChange={(event) => setTitle(event.target.value)}
                        onKeyDown={(event) => {
                            if (event.key === 'Enter') {
                                event.preventDefault();
                                void handleSubmit();
                            }
                        }}
                        placeholder="Section title"
                        value={title}
                    />
                </div>
                <DialogFooter>
                    <Button
                        disabled={submitting || saving}
                        onClick={() => onOpenChange(false)}
                        type="button"
                        variant="outline"
                    >
                        Cancel
                    </Button>
                    <Button
                        disabled={!title.trim() || submitting || saving}
                        onClick={() => {
                            void handleSubmit();
                        }}
                        type="button"
                    >
                        {submitting ? <Spinner className="size-4" /> : null}
                        Add section
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

// The row of per-section actions the enterprise edition shows on hover. Hidden
// from the pointer until the header is hovered or focused; a hidden section keeps
// its own row visible so the "show" affordance stays reachable.
function FlowReportSectionActions({ boundaries, busy, editor, onEdit, section }: FlowReportSectionActionsProps) {
    const [addPlacement, setAddPlacement] = useState<'after' | 'child' | null>(null);
    const [deleteOpen, setDeleteOpen] = useState(false);

    const disabled = busy;

    return (
        <div
            className={cn(
                'flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-focus-within/report-section:opacity-100 group-hover/report-section:opacity-100 max-md:opacity-100',
                (section.hidden || deleteOpen) && 'opacity-100',
            )}
        >
            <Button
                aria-label="Move section up"
                disabled={disabled || !boundaries.canMoveUp}
                onClick={() => {
                    void editor.moveSection(section.id, 'up');
                }}
                size="icon-sm"
                title="Move up"
                variant="ghost"
            >
                <ArrowUp />
            </Button>
            <Button
                aria-label="Move section down"
                disabled={disabled || !boundaries.canMoveDown}
                onClick={() => {
                    void editor.moveSection(section.id, 'down');
                }}
                size="icon-sm"
                title="Move down"
                variant="ghost"
            >
                <ArrowDown />
            </Button>
            <Button
                aria-label={section.hidden ? 'Show section' : 'Hide section'}
                disabled={disabled}
                onClick={() => {
                    void editor.setSectionHidden(section.id, !section.hidden);
                }}
                size="icon-sm"
                title={section.hidden ? 'Show in the report' : 'Hide from the report'}
                variant="ghost"
            >
                {section.hidden ? <Eye /> : <EyeOff />}
            </Button>
            <Button
                aria-label="Edit section"
                disabled={disabled}
                onClick={onEdit}
                size="icon-sm"
                title="Edit"
                variant="ghost"
            >
                <Pencil />
            </Button>
            <DropdownMenu>
                <DropdownMenuTrigger asChild>
                    <Button
                        aria-label="Add section"
                        disabled={disabled}
                        size="icon-sm"
                        title="Add a section"
                        variant="ghost"
                    >
                        <Plus />
                    </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                    <DropdownMenuItem onSelect={() => setAddPlacement('after')}>Add section after</DropdownMenuItem>
                    <DropdownMenuItem onSelect={() => setAddPlacement('child')}>Add subsection</DropdownMenuItem>
                </DropdownMenuContent>
            </DropdownMenu>
            <Button
                aria-label="Delete section"
                disabled={disabled}
                onClick={() => setDeleteOpen(true)}
                size="icon-sm"
                title="Delete"
                variant="ghost"
            >
                <Trash2 />
            </Button>

            <FlowReportAddSectionDialog
                onOpenChange={(open) => {
                    if (!open) {
                        setAddPlacement(null);
                    }
                }}
                onSubmit={(title) =>
                    editor.addSection({
                        afterSectionId: addPlacement === 'after' ? section.id : null,
                        parentId: addPlacement === 'child' ? section.id : null,
                        title,
                    })
                }
                open={addPlacement !== null}
                placement={addPlacement === 'child' ? 'subsection of this section' : 'after this section'}
                saving={busy}
            />

            <ConfirmationDialog
                confirmText="Delete"
                description={`Delete "${section.title}"? Any subsections it holds are removed too.`}
                handleConfirm={async () => {
                    await editor.deleteSection(section.id);
                }}
                handleOpenChange={setDeleteOpen}
                isOpen={deleteOpen}
                itemName={section.title}
                itemType="section"
                title="Delete section"
            />
        </div>
    );
}

export default FlowReportSectionActions;
