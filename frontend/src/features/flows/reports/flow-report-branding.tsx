import { ChevronRight, ImageOff, Trash2, Upload } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Spinner } from '@/components/ui/spinner';
import { cn } from '@/lib/utils';

import { type BrandingLogoFile, readFileAsDataUrl, validateBrandingLogoFile } from './report-branding';
import { useFlowReportBranding } from './use-flow-report-branding';

interface FlowReportBrandingProps {
    flowId: null | string;
    reportId: null | string;
}

// The export branding of one report (plan § 6.6 / P6). Unlike the sections, it
// takes no part in the generation, so it stays editable whatever the run status.
function FlowReportBranding({ flowId, reportId }: FlowReportBrandingProps) {
    const { branding, loading, logoUrl, save, saving } = useFlowReportBranding(reportId, flowId);

    const [open, setOpen] = useState(false);
    const [title, setTitle] = useState('');
    const [subtitle, setSubtitle] = useState('');
    const [header, setHeader] = useState('');
    const [footer, setFooter] = useState('');
    const [logoFile, setLogoFile] = useState<BrandingLogoFile | null>(null);
    const [removed, setRemoved] = useState(false);
    const [logoError, setLogoError] = useState<null | string>(null);
    const fileInputRef = useRef<HTMLInputElement>(null);
    const loadedRef = useRef(false);

    const hydrate = useCallback(() => {
        setTitle(branding?.title ?? '');
        setSubtitle(branding?.subtitle ?? '');
        setHeader(branding?.header ?? '');
        setFooter(branding?.footer ?? '');
        setLogoFile(null);
        setRemoved(false);
        setLogoError(null);
    }, [branding]);

    // Seed the form once the branding lands; later refetches must not clobber
    // what the user is typing.
    useEffect(() => {
        if (!branding || loadedRef.current) {
            return;
        }

        loadedRef.current = true;
        hydrate();
    }, [branding, hydrate]);

    const handleFile = useCallback(async (file: File | undefined) => {
        if (!file) {
            return;
        }

        const error = validateBrandingLogoFile(file);

        setLogoError(error);

        if (error) {
            return;
        }

        try {
            const dataUrl = await readFileAsDataUrl(file);

            setLogoFile({ dataUrl, mediaType: file.type, name: file.name });
            setRemoved(false);
        } catch {
            setLogoError('The image could not be read.');
        }
    }, []);

    const handleSave = useCallback(async () => {
        const ok = await save({
            footer,
            header,
            logo: logoFile ? logoFile.dataUrl : removed ? '' : null,
            logoName: logoFile?.name ?? null,
            subtitle,
            title,
        });

        if (ok) {
            setLogoFile(null);
            setRemoved(false);
            toast.success('Branding saved');
        }
    }, [footer, header, logoFile, removed, save, subtitle, title]);

    const hasStoredLogo = !!branding?.hasLogo && !removed;
    const previewSrc = logoFile?.dataUrl ?? (hasStoredLogo ? logoUrl : null);

    return (
        <Collapsible
            className="bg-muted/40 rounded-lg border"
            onOpenChange={setOpen}
            open={open}
        >
            <CollapsibleTrigger asChild>
                <button
                    className="hover:bg-muted/60 flex w-full items-center gap-1 rounded-lg px-3 py-2 text-left text-xs font-medium tracking-wide uppercase"
                    type="button"
                >
                    <ChevronRight className={cn('size-3.5 transition-transform', open && 'rotate-90')} />
                    Branding
                </button>
            </CollapsibleTrigger>

            <CollapsibleContent>
                <div className="flex flex-col gap-3 border-t px-3 py-3">
                    {loading && !branding ? (
                        <div className="text-muted-foreground flex items-center gap-2 text-xs">
                            <Spinner className="size-3" />
                            Loading branding…
                        </div>
                    ) : (
                        <>
                            <div className="flex flex-col gap-1.5">
                                <Label htmlFor="report-branding-title">Cover title</Label>
                                <Input
                                    id="report-branding-title"
                                    onChange={(event) => setTitle(event.target.value)}
                                    placeholder="The report's own title"
                                    value={title}
                                />
                            </div>

                            <div className="flex flex-col gap-1.5">
                                <Label htmlFor="report-branding-subtitle">Cover subtitle</Label>
                                <Input
                                    id="report-branding-subtitle"
                                    onChange={(event) => setSubtitle(event.target.value)}
                                    placeholder="Client, engagement reference…"
                                    value={subtitle}
                                />
                            </div>

                            <div className="flex flex-col gap-1.5">
                                <Label htmlFor="report-branding-header">Page header</Label>
                                <Input
                                    id="report-branding-header"
                                    onChange={(event) => setHeader(event.target.value)}
                                    placeholder="Defaults to the cover title"
                                    value={header}
                                />
                            </div>

                            <div className="flex flex-col gap-1.5">
                                <Label htmlFor="report-branding-footer">Page footer</Label>
                                <Input
                                    id="report-branding-footer"
                                    onChange={(event) => setFooter(event.target.value)}
                                    placeholder="Generated by PentAGI · date"
                                    value={footer}
                                />
                                <p className="text-muted-foreground text-xs">
                                    {'[[page]] and [[pages]] print the page numbers; [[date]] prints the report date.'}
                                </p>
                            </div>

                            <div className="flex flex-col gap-1.5">
                                <Label>Logo</Label>
                                <div className="flex items-center gap-3">
                                    <div className="bg-background flex size-16 shrink-0 items-center justify-center overflow-hidden rounded-md border">
                                        {previewSrc ? (
                                            <img
                                                alt="Report logo"
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
                                            {previewSrc ? 'Replace' : 'Choose a file'}
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
                                                Remove
                                            </Button>
                                        )}
                                    </div>
                                </div>
                                {logoError && <p className="text-destructive text-xs">{logoError}</p>}
                                <p className="text-muted-foreground text-xs">
                                    PNG or JPEG, up to 256 KB. The logo appears on the Word and PDF covers and travels
                                    in the archive.
                                </p>
                            </div>

                            <div className="flex items-center justify-end gap-2">
                                <Button
                                    disabled={saving}
                                    onClick={hydrate}
                                    size="sm"
                                    type="button"
                                    variant="ghost"
                                >
                                    Cancel
                                </Button>
                                <Button
                                    disabled={saving}
                                    onClick={() => {
                                        void handleSave();
                                    }}
                                    size="sm"
                                    type="button"
                                >
                                    {saving ? <Spinner className="size-4" /> : null}
                                    Save
                                </Button>
                            </div>
                        </>
                    )}
                </div>
            </CollapsibleContent>
        </Collapsible>
    );
}

export default FlowReportBranding;
