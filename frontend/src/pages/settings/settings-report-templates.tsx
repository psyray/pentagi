import type { ColumnDef } from '@tanstack/react-table';

import {
    Ellipsis,
    FilePlus2,
    FileText,
    ImageOff,
    Palette,
    Pencil,
    Plus,
    Power,
    PowerOff,
    Trash2,
    TriangleAlert,
    Upload,
} from 'lucide-react';
import { useRef, useState } from 'react';
import { toast } from 'sonner';

import type { BadgeVariant } from '@/components/ui/badge';
import type { BrandingLogoFile } from '@/features/flows/reports/report-branding';
import type { ReportTemplateBrandingValues } from '@/features/reports/use-report-templates';
import type { ReportTemplateFragmentFragment } from '@/graphql/types';

import {
    AppHeader,
    AppHeaderAction,
    AppHeaderActions,
    AppHeaderContent,
    AppHeaderTitle,
} from '@/components/layouts/app/app-header';
import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import { ErrorState } from '@/components/shared/error-state';
import { LoadingState } from '@/components/shared/loading-state';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { ContextMenuItem, ContextMenuSeparator } from '@/components/ui/context-menu';
import { DataTable } from '@/components/ui/data-table';
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
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Spinner } from '@/components/ui/spinner';
import { readFileAsDataUrl, validateBrandingLogoFile } from '@/features/flows/reports/report-branding';
import { useReportPermissions } from '@/features/flows/reports/use-report-permissions';
import { reportTemplateLogoUrl } from '@/features/reports/report-template-logo';
import { useReportTemplates } from '@/features/reports/use-report-templates';
import { YamlEditor } from '@/features/reports/yaml-editor';
import { ReportTemplateSource } from '@/graphql/types';
import { usePageStorageKeys } from '@/hooks/use-page-storage-keys';
import { useUser } from '@/providers/user-provider';

// Where a listed template comes from, as shown in the "Source" column.
const SOURCE_META: Record<ReportTemplateSource, { label: string; variant: BadgeVariant }> = {
    [ReportTemplateSource.Bundled]: { label: 'Bundled', variant: 'secondary' },
    [ReportTemplateSource.Global]: { label: 'Global', variant: 'purple' },
    [ReportTemplateSource.User]: { label: 'Mine', variant: 'blue' },
};

const errorMessage = (error: unknown, fallback: string): string =>
    error instanceof Error && error.message ? error.message : fallback;

const isBundled = (template: ReportTemplateFragmentFragment): boolean =>
    template.source === ReportTemplateSource.Bundled;

type BrandingDialogBodyProps = Omit<BrandingDialogProps, 'open'>;

interface BrandingDialogProps {
    onOpenChange: (open: boolean) => void;
    /** Rejects with the GraphQL error to keep the dialog open and show it. */
    onSubmit: (values: ReportTemplateBrandingValues) => Promise<void>;
    open: boolean;
    template: null | ReportTemplateFragmentFragment;
}

type TemplateEditorDialogBodyProps = Omit<TemplateEditorDialogProps, 'open'>;

interface TemplateEditorDialogProps {
    isAdmin: boolean;
    onOpenChange: (open: boolean) => void;
    /** Rejects with the GraphQL error to keep the dialog open and show it. */
    onSubmit: (values: { global: boolean; yaml: string }) => Promise<void>;
    open: boolean;
    /** The template being edited; null opens the "New template" editor. */
    template: null | ReportTemplateFragmentFragment;
}

function BrandingDialog({ onOpenChange, onSubmit, open, template }: BrandingDialogProps) {
    return (
        <Dialog
            onOpenChange={onOpenChange}
            open={open}
        >
            <DialogContent>
                <BrandingDialogBody
                    key={template?.id ?? 'none'}
                    onOpenChange={onOpenChange}
                    onSubmit={onSubmit}
                    template={template}
                />
            </DialogContent>
        </Dialog>
    );
}

function BrandingDialogBody({ onOpenChange, onSubmit, template }: BrandingDialogBodyProps) {
    const branding = template?.branding;

    const [error, setError] = useState<null | string>(null);
    const [footer, setFooter] = useState(branding?.footer ?? '');
    const [header, setHeader] = useState(branding?.header ?? '');
    const [logoError, setLogoError] = useState<null | string>(null);
    const [logoFile, setLogoFile] = useState<BrandingLogoFile | null>(null);
    const [removed, setRemoved] = useState(false);
    const [saving, setSaving] = useState(false);
    const [subtitle, setSubtitle] = useState(branding?.subtitle ?? '');
    const [title, setTitle] = useState(branding?.title ?? '');
    const fileInputRef = useRef<HTMLInputElement>(null);

    const handleFile = async (file: File | undefined) => {
        if (!file) {
            return;
        }

        const validation = validateBrandingLogoFile(file);

        setLogoError(validation);

        if (validation) {
            return;
        }

        try {
            const dataUrl = await readFileAsDataUrl(file);

            setLogoFile({ dataUrl, mediaType: file.type, name: file.name });
            setRemoved(false);
        } catch {
            setLogoError('The image could not be read.');
        }
    };

    const handleSave = async () => {
        setError(null);
        setSaving(true);

        try {
            await onSubmit({
                footer,
                header,
                logo: logoFile ? logoFile.dataUrl : removed ? '' : null,
                logoName: logoFile?.name ?? null,
                subtitle,
                title,
            });
            onOpenChange(false);
        } catch (submitError) {
            setError(errorMessage(submitError, 'The branding could not be saved.'));
        } finally {
            setSaving(false);
        }
    };

    // A just-chosen file wins; otherwise the stored logo is fetched from the API
    // endpoint (cookie-authenticated). The URL is cache-busted by updatedAt so an
    // upload shows up without a hard refresh.
    const hasStoredLogo = !!branding?.hasLogo && !removed;
    const previewSrc =
        logoFile?.dataUrl ??
        (hasStoredLogo ? reportTemplateLogoUrl(template?.id ?? '', template?.updatedAt ?? undefined) : null);

    return (
        <>
            <DialogHeader>
                <DialogTitle>Template branding</DialogTitle>
                <DialogDescription>Branding handed down to the reports generated from this template.</DialogDescription>
            </DialogHeader>

            <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="template-branding-title">Cover title</Label>
                    <Input
                        id="template-branding-title"
                        onChange={(event) => setTitle(event.target.value)}
                        placeholder="The report's own title"
                        value={title}
                    />
                </div>

                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="template-branding-subtitle">Cover subtitle</Label>
                    <Input
                        id="template-branding-subtitle"
                        onChange={(event) => setSubtitle(event.target.value)}
                        placeholder="Client, engagement reference…"
                        value={subtitle}
                    />
                </div>

                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="template-branding-header">Page header</Label>
                    <Input
                        id="template-branding-header"
                        onChange={(event) => setHeader(event.target.value)}
                        placeholder="Defaults to the cover title"
                        value={header}
                    />
                </div>

                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="template-branding-footer">Page footer</Label>
                    <Input
                        id="template-branding-footer"
                        onChange={(event) => setFooter(event.target.value)}
                        placeholder="Generated by PentAGI · date"
                        value={footer}
                    />
                </div>

                <div className="flex flex-col gap-1.5">
                    <Label>Logo</Label>
                    <div className="flex items-center gap-3">
                        <div className="bg-background flex h-16 w-24 shrink-0 items-center justify-center overflow-hidden rounded-md border px-2 text-center">
                            {previewSrc ? (
                                <img
                                    alt="Template logo preview"
                                    className="max-h-full max-w-full object-contain"
                                    src={previewSrc}
                                />
                            ) : (
                                <ImageOff className="text-muted-foreground size-5" />
                            )}
                        </div>
                        <div className="flex flex-col gap-1.5">
                            <input
                                accept="image/png,image/jpeg"
                                className="hidden"
                                onChange={(event) => {
                                    void handleFile(event.target.files?.[0]);
                                    event.target.value = '';
                                }}
                                ref={fileInputRef}
                                type="file"
                            />
                            <Button
                                onClick={() => fileInputRef.current?.click()}
                                size="sm"
                                type="button"
                                variant="outline"
                            >
                                <Upload className="size-3.5" />
                                {hasStoredLogo || logoFile ? 'Replace' : 'Choose a file'}
                            </Button>
                            {(hasStoredLogo || logoFile) && (
                                <Button
                                    onClick={() => {
                                        setLogoFile(null);
                                        setRemoved(true);
                                    }}
                                    size="sm"
                                    type="button"
                                    variant="ghost"
                                >
                                    <Trash2 className="size-3.5" />
                                    Remove logo
                                </Button>
                            )}
                        </div>
                    </div>
                    {logoError && <p className="text-destructive text-xs">{logoError}</p>}
                    <p className="text-muted-foreground text-xs">PNG or JPEG, up to 256 KB.</p>
                </div>

                {error && (
                    <Alert variant="destructive">
                        <TriangleAlert />
                        <AlertTitle>Could not save the branding</AlertTitle>
                        <AlertDescription>{error}</AlertDescription>
                    </Alert>
                )}
            </div>

            <DialogFooter>
                <Button
                    onClick={() => onOpenChange(false)}
                    type="button"
                    variant="ghost"
                >
                    Cancel
                </Button>
                <Button
                    disabled={saving}
                    onClick={() => void handleSave()}
                    type="button"
                >
                    {saving ? <Spinner className="size-4" /> : null}
                    Save
                </Button>
            </DialogFooter>
        </>
    );
}

function SettingsReportTemplates() {
    const {
        createTemplate,
        deleteTemplate,
        error,
        loading,
        refetch,
        saveBranding,
        setEnabled,
        templates,
        updateTemplate,
    } = useReportTemplates();
    const permissions = useReportPermissions();
    const { authInfo } = useUser();
    const isAdmin = authInfo?.privileges?.includes('reports.admin') ?? false;
    const { table: tableStorageBase } = usePageStorageKeys();

    const [brandingOpen, setBrandingOpen] = useState(false);
    const [brandingTemplate, setBrandingTemplate] = useState<null | ReportTemplateFragmentFragment>(null);
    const [busyId, setBusyId] = useState<null | string>(null);
    const [deleteOpen, setDeleteOpen] = useState(false);
    const [deletingTemplate, setDeletingTemplate] = useState<null | ReportTemplateFragmentFragment>(null);
    const [editorOpen, setEditorOpen] = useState(false);
    const [editorTemplate, setEditorTemplate] = useState<null | ReportTemplateFragmentFragment>(null);

    // Edit/branding/toggle/delete all need reports.edit; a global row additionally
    // needs reports.admin. A bundled row keeps only the read-only YAML view.
    const canManageRow = (template: ReportTemplateFragmentFragment): boolean =>
        permissions.canEdit && (template.source !== ReportTemplateSource.Global || isAdmin);

    const openCreate = () => {
        setEditorTemplate(null);
        setEditorOpen(true);
    };

    const openEdit = (template: ReportTemplateFragmentFragment) => {
        setEditorTemplate(template);
        setEditorOpen(true);
    };

    const openBranding = (template: ReportTemplateFragmentFragment) => {
        setBrandingTemplate(template);
        setBrandingOpen(true);
    };

    const openDelete = (template: ReportTemplateFragmentFragment) => {
        setDeletingTemplate(template);
        setDeleteOpen(true);
    };

    const handleEditorSubmit = async (values: { global: boolean; yaml: string }) => {
        if (editorTemplate) {
            await updateTemplate(editorTemplate, values.yaml);
            toast.success('Report template saved');
        } else {
            await createTemplate({ global: isAdmin && values.global, yaml: values.yaml });
            toast.success('Report template created');
        }
    };

    const handleBrandingSubmit = async (values: ReportTemplateBrandingValues) => {
        if (!brandingTemplate) {
            return;
        }

        await saveBranding(brandingTemplate, values);
        toast.success('Branding saved');
    };

    const handleToggle = async (template: ReportTemplateFragmentFragment) => {
        setBusyId(template.id);

        try {
            await setEnabled(template, !template.enabled);
            toast.success(template.enabled ? 'Template disabled' : 'Template enabled');
        } catch (toggleError) {
            toast.error('Failed to update the template', {
                description: errorMessage(toggleError, 'Please try again.'),
            });
        } finally {
            setBusyId(null);
        }
    };

    const handleDeleteConfirm = async () => {
        if (!deletingTemplate) {
            return;
        }

        try {
            await deleteTemplate(deletingTemplate);
            toast.success('Report template deleted');
            setDeletingTemplate(null);
        } catch (deleteError) {
            toast.error('Failed to delete the template', {
                description: errorMessage(deleteError, 'Please try again.'),
            });
        }
    };

    const columns: ColumnDef<ReportTemplateFragmentFragment>[] = [
        {
            accessorKey: 'label',
            cell: ({ row }) => {
                const template = row.original;

                return (
                    <div className="flex min-w-0 flex-col">
                        <span
                            className="truncate font-medium"
                            title={template.label}
                        >
                            {template.label}
                        </span>
                        <span
                            className="text-muted-foreground truncate font-mono text-xs"
                            title={template.key}
                        >
                            {template.key}
                        </span>
                    </div>
                );
            },
            enableHiding: false,
            header: 'Template',
            meta: { columnMenuLabel: 'Template', searchable: true },
            minSize: 200,
            size: 320,
        },
        {
            accessorKey: 'source',
            cell: ({ row }) => {
                const meta = SOURCE_META[row.original.source];

                return <Badge variant={meta.variant}>{meta.label}</Badge>;
            },
            header: 'Source',
            maxSize: 140,
            meta: { columnMenuLabel: 'Source', searchable: true },
            minSize: 90,
            size: 120,
        },
        {
            accessorKey: 'version',
            cell: ({ row }) => <span className="text-muted-foreground text-sm">v{row.original.version}</span>,
            header: 'Version',
            maxSize: 110,
            meta: { columnMenuLabel: 'Version' },
            minSize: 70,
            size: 90,
        },
        {
            accessorKey: 'enabled',
            cell: ({ row }) => (
                <Badge variant={row.original.enabled ? 'green' : 'secondary'}>
                    {row.original.enabled ? 'Enabled' : 'Disabled'}
                </Badge>
            ),
            header: 'Status',
            maxSize: 130,
            meta: { columnMenuLabel: 'Status' },
            minSize: 90,
            size: 110,
        },
        {
            cell: ({ row }) => {
                const template = row.original;

                if (!canManageRow(template)) {
                    return null;
                }

                return (
                    <div className="flex items-center justify-end opacity-0 transition-opacity group-hover:opacity-100">
                        <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                                <Button
                                    aria-label="Open menu"
                                    className="size-8 p-0"
                                    variant="ghost"
                                >
                                    <Ellipsis />
                                </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent
                                align="end"
                                className="min-w-24"
                            >
                                <DropdownMenuItem onClick={() => openEdit(template)}>
                                    <Pencil className="size-3" />
                                    Edit YAML
                                </DropdownMenuItem>
                                {!isBundled(template) && (
                                    <>
                                        <DropdownMenuSeparator />
                                        <DropdownMenuItem onClick={() => openBranding(template)}>
                                            <Palette className="size-3" />
                                            Branding
                                        </DropdownMenuItem>
                                        <DropdownMenuItem
                                            disabled={busyId === template.id}
                                            onClick={() => void handleToggle(template)}
                                        >
                                            {template.enabled ? (
                                                <PowerOff className="size-3" />
                                            ) : (
                                                <Power className="size-3" />
                                            )}
                                            {template.enabled ? 'Disable' : 'Enable'}
                                        </DropdownMenuItem>
                                        <DropdownMenuSeparator />
                                        <DropdownMenuItem onClick={() => openDelete(template)}>
                                            <Trash2 className="size-3" />
                                            Delete
                                        </DropdownMenuItem>
                                    </>
                                )}
                            </DropdownMenuContent>
                        </DropdownMenu>
                    </div>
                );
            },
            enableHiding: false,
            header: () => null,
            id: 'actions',
            maxSize: 70,
            meta: { preventRowClick: true },
            minSize: 50,
            size: 60,
        },
    ];

    const renderRowContextMenu = (template: ReportTemplateFragmentFragment) => {
        if (!canManageRow(template)) {
            return null;
        }

        return (
            <>
                <ContextMenuItem onClick={() => openEdit(template)}>
                    <Pencil />
                    Edit YAML
                </ContextMenuItem>
                {!isBundled(template) && (
                    <>
                        <ContextMenuSeparator />
                        <ContextMenuItem onClick={() => openBranding(template)}>
                            <Palette />
                            Branding
                        </ContextMenuItem>
                        <ContextMenuItem
                            disabled={busyId === template.id}
                            onClick={() => void handleToggle(template)}
                        >
                            {template.enabled ? <PowerOff /> : <Power />}
                            {template.enabled ? 'Disable' : 'Enable'}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem onClick={() => openDelete(template)}>
                            <Trash2 />
                            Delete
                        </ContextMenuItem>
                    </>
                )}
            </>
        );
    };

    const pageHeader = (
        <AppHeader>
            <AppHeaderContent>
                <AppHeaderTitle icon={<FileText className="size-4 shrink-0" />}>Report templates</AppHeaderTitle>
            </AppHeaderContent>
            {permissions.canEdit && (
                <AppHeaderActions>
                    <AppHeaderAction
                        icon={<Plus />}
                        label="New template"
                        onClick={openCreate}
                        variant="secondary"
                    />
                </AppHeaderActions>
            )}
        </AppHeader>
    );

    if (!permissions.canRead) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <Empty>
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <FileText />
                            </EmptyMedia>
                            <EmptyTitle>Report templates unavailable</EmptyTitle>
                            <EmptyDescription>
                                Your account does not have permission to view report templates.
                            </EmptyDescription>
                        </EmptyHeader>
                    </Empty>
                </div>
            </>
        );
    }

    if (loading && !templates.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <LoadingState
                        description="Please wait while we fetch your report templates"
                        title="Loading report templates..."
                    />
                </div>
            </>
        );
    }

    // Error surface only when there's no data — a failed background refetch must not blank a working list.
    if (error && !templates.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <ErrorState
                        message={error.message}
                        onRetry={refetch}
                        title="Error loading report templates"
                    />
                </div>
            </>
        );
    }

    if (!templates.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <Empty>
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <FileText />
                            </EmptyMedia>
                            <EmptyTitle>No report templates</EmptyTitle>
                            <EmptyDescription>
                                Bundled templates ship with PentAGI; add your own to customise the report generation.
                            </EmptyDescription>
                        </EmptyHeader>
                        {permissions.canEdit && (
                            <EmptyContent>
                                <Button
                                    onClick={openCreate}
                                    variant="secondary"
                                >
                                    <Plus />
                                    New template
                                </Button>
                            </EmptyContent>
                        )}
                    </Empty>
                </div>
            </>
        );
    }

    return (
        <>
            {pageHeader}
            <div className="flex flex-col gap-4 p-4 pt-0">
                <DataTable<ReportTemplateFragmentFragment>
                    columns={columns}
                    data={templates}
                    empty={{ entityName: 'report templates' }}
                    filterPlaceholder="Filter templates..."
                    initialPageSize={1000}
                    label="Report templates"
                    renderRowContextMenu={renderRowContextMenu}
                    storageKey={`${tableStorageBase}:templates`}
                />

                <TemplateEditorDialog
                    isAdmin={isAdmin}
                    onOpenChange={setEditorOpen}
                    onSubmit={handleEditorSubmit}
                    open={editorOpen}
                    template={editorTemplate}
                />

                <BrandingDialog
                    onOpenChange={setBrandingOpen}
                    onSubmit={handleBrandingSubmit}
                    open={brandingOpen}
                    template={brandingTemplate}
                />

                <ConfirmationDialog
                    cancelText="Cancel"
                    confirmText="Delete"
                    description={`Delete "${deletingTemplate?.label || 'this template'}"? Reports already generated from it keep their content, but the template is removed for good.`}
                    handleConfirm={handleDeleteConfirm}
                    handleOpenChange={setDeleteOpen}
                    isOpen={deleteOpen}
                    itemName={deletingTemplate?.label || 'this template'}
                    itemType="report template"
                    title="Delete report template"
                />
            </div>
        </>
    );
}

function TemplateEditorDialog({ isAdmin, onOpenChange, onSubmit, open, template }: TemplateEditorDialogProps) {
    return (
        <Dialog
            onOpenChange={onOpenChange}
            open={open}
        >
            <DialogContent className="max-w-[95%] sm:max-w-[95%] xl:max-w-[80%]">
                <TemplateEditorDialogBody
                    isAdmin={isAdmin}
                    key={template?.id ?? 'new'}
                    onOpenChange={onOpenChange}
                    onSubmit={onSubmit}
                    template={template}
                />
            </DialogContent>
        </Dialog>
    );
}

function TemplateEditorDialogBody({ isAdmin, onOpenChange, onSubmit, template }: TemplateEditorDialogBodyProps) {
    const bundled = !!template && isBundled(template);
    const isCreate = !template;

    const [error, setError] = useState<null | string>(null);
    const [makeGlobal, setMakeGlobal] = useState(template?.source === ReportTemplateSource.Global);
    const [saving, setSaving] = useState(false);
    const [yaml, setYaml] = useState(template?.yaml ?? '');

    const handleSave = async () => {
        setError(null);
        setSaving(true);

        try {
            await onSubmit({ global: isAdmin && makeGlobal, yaml });
            onOpenChange(false);
        } catch (submitError) {
            setError(errorMessage(submitError, 'The template could not be saved.'));
        } finally {
            setSaving(false);
        }
    };

    return (
        <>
            <DialogHeader>
                <DialogTitle>{isCreate ? 'New report template' : template?.label}</DialogTitle>
                <DialogDescription>
                    The YAML document is the template. Its own key, label and version are authoritative — they are read
                    back from the document by the server on save.
                </DialogDescription>
            </DialogHeader>

            <div className="flex min-w-0 flex-col gap-3">
                {bundled && (
                    <Alert>
                        <FileText />
                        <AlertTitle>Built-in template</AlertTitle>
                        <AlertDescription>
                            This template ships with PentAGI and is read-only. Create your own template to customise it.
                        </AlertDescription>
                    </Alert>
                )}

                <YamlEditor
                    aria-label="Report template YAML"
                    onChange={setYaml}
                    readOnly={bundled}
                    value={yaml}
                />

                {isCreate && isAdmin && (
                    <label className="flex items-center gap-2 text-sm">
                        <Checkbox
                            checked={makeGlobal}
                            onCheckedChange={(checked) => setMakeGlobal(checked === true)}
                        />
                        Make it global (admins only)
                    </label>
                )}

                {error && (
                    <Alert variant="destructive">
                        <TriangleAlert />
                        <AlertTitle>Could not save the template</AlertTitle>
                        <AlertDescription>{error}</AlertDescription>
                    </Alert>
                )}
            </div>

            <DialogFooter>
                <Button
                    onClick={() => onOpenChange(false)}
                    type="button"
                    variant="ghost"
                >
                    Cancel
                </Button>
                <Button
                    disabled={bundled || saving}
                    onClick={() => void handleSave()}
                    type="button"
                >
                    {saving ? <Spinner className="size-4" /> : <FilePlus2 className="size-4" />}
                    {isCreate ? 'Create template' : 'Save'}
                </Button>
            </DialogFooter>
        </>
    );
}

export default SettingsReportTemplates;
