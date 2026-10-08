import { Plus, X } from 'lucide-react';
import { useState } from 'react';

import { type EditorViewMode, EditorViewModeToggle, MarkdownEditorField } from '@/components/shared/markdown-editor';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Spinner } from '@/components/ui/spinner';
import { Textarea } from '@/components/ui/textarea';
import { type ReportSectionFragmentFragment } from '@/graphql/types';

import type { SaveReportSectionInput } from './use-flow-report-editor';

import { parseVariableRows, type ReportVariableRow } from './report-section-edit';

interface FlowReportSectionEditorProps {
    onCancel: () => void;
    onSaved: () => void;
    save: (input: SaveReportSectionInput) => Promise<boolean>;
    saving: boolean;
    section: ReportSectionFragmentFragment;
}

function FlowReportSectionEditor({ onCancel, onSaved, save, saving, section }: FlowReportSectionEditorProps) {
    const [title, setTitle] = useState(section.title);
    const [instructions, setInstructions] = useState(section.instructions ?? '');
    const [content, setContent] = useState(section.content);
    const [rows, setRows] = useState<ReportVariableRow[]>(() => parseVariableRows(section.variables));
    const [viewMode, setViewMode] = useState<EditorViewMode>('rich');

    const canSave = title.trim().length > 0 && !saving;

    const updateRow = (index: number, patch: Partial<ReportVariableRow>) => {
        setRows((current) => current.map((row, at) => (at === index ? { ...row, ...patch } : row)));
    };

    const removeRow = (index: number) => {
        setRows((current) => current.filter((_, at) => at !== index));
    };

    const addRow = () => {
        setRows((current) => [...current, { name: '', value: '' }]);
    };

    const handleSave = async () => {
        const ok = await save({
            content,
            instructions: instructions.trim() ? instructions : null,
            originalContent: section.content,
            sectionId: section.id,
            title: title.trim(),
            variables: rows,
            warnings: section.warnings ?? null,
        });

        if (ok) {
            onSaved();
        }
    };

    return (
        <div className="bg-muted/30 mt-2 flex flex-col gap-3 rounded-lg border p-3">
            <div className="flex flex-col gap-1.5">
                <Label htmlFor={`report-section-title-${section.id}`}>Title</Label>
                <Input
                    id={`report-section-title-${section.id}`}
                    onChange={(event) => setTitle(event.target.value)}
                    value={title}
                />
            </div>

            <div className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between gap-2">
                    <Label>Variables</Label>
                    <Button
                        onClick={addRow}
                        size="sm"
                        type="button"
                        variant="outline"
                    >
                        <Plus className="size-3.5" />
                        Add
                    </Button>
                </div>
                {rows.length === 0 ? (
                    <p className="text-muted-foreground text-xs">
                        This section declares no values of its own. A value is written as JSON: the quotes are the type
                        (9.8 is a number, &quot;443&quot; is a port written as text). Referenced in the content as{' '}
                        {'{{.Name}}'}.
                    </p>
                ) : (
                    <div className="flex flex-col gap-2">
                        {rows.map((row, index) => (
                            <div
                                className="flex items-center gap-2"
                                key={`${section.id}-var-${index}`}
                            >
                                <Input
                                    aria-label="Variable name"
                                    className="max-w-48"
                                    onChange={(event) => updateRow(index, { name: event.target.value })}
                                    placeholder="Name"
                                    value={row.name}
                                />
                                <Input
                                    aria-label="Variable value"
                                    onChange={(event) => updateRow(index, { value: event.target.value })}
                                    placeholder="Value (JSON)"
                                    value={row.value}
                                />
                                <Button
                                    aria-label="Remove variable"
                                    onClick={() => removeRow(index)}
                                    size="icon"
                                    type="button"
                                    variant="ghost"
                                >
                                    <X className="size-4" />
                                </Button>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            <div className="flex flex-col gap-1.5">
                <Label htmlFor={`report-section-instructions-${section.id}`}>Instructions</Label>
                <Textarea
                    id={`report-section-instructions-${section.id}`}
                    onChange={(event) => setInstructions(event.target.value)}
                    placeholder="Guidance kept for a later re-preparation of this section."
                    rows={2}
                    value={instructions}
                />
            </div>

            <div className="flex flex-col gap-1.5">
                <div className="flex items-center justify-between gap-2">
                    <Label htmlFor={`report-section-content-${section.id}`}>Content</Label>
                    <EditorViewModeToggle
                        mode={viewMode}
                        onModeChange={setViewMode}
                        rawTooltip="Edit the raw markdown"
                    />
                </div>
                <MarkdownEditorField
                    aria-label="Section content"
                    className="h-auto max-h-[70dvh] min-h-[16rem] xl:min-h-[16rem] xl:flex-none"
                    id={`report-section-content-${section.id}`}
                    mode={viewMode}
                    onChange={setContent}
                    value={content}
                />
                <p className="text-muted-foreground text-xs">
                    Markdown. Placeholders are resolved when the report is read or exported.
                </p>
            </div>

            <div className="flex items-center justify-end gap-2">
                <Button
                    disabled={saving}
                    onClick={onCancel}
                    type="button"
                    variant="ghost"
                >
                    Cancel
                </Button>
                <Button
                    disabled={!canSave}
                    onClick={() => {
                        void handleSave();
                    }}
                    type="button"
                >
                    {saving ? <Spinner className="size-4" /> : null}
                    Save
                </Button>
            </div>
        </div>
    );
}

export default FlowReportSectionEditor;
