import { useQuery } from '@apollo/client/react';
import { zodResolver } from '@hookform/resolvers/zod';
import { FilePlus2, TriangleAlert } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Form, FormControl, FormField, FormItem, FormLabel } from '@/components/ui/form';
import { FormSubmitButton } from '@/components/ui/form-submit-button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { ReportTemplatesDocument } from '@/graphql/types';
import { Log } from '@/lib/log';
import { useProviders } from '@/providers/providers-provider';

import { type CreateReportInput, type ReportProgress } from './use-flow-reports';

const DEFAULT_TEMPLATE_KEY = 'pentagi-default-pentest-report';
const DEFAULT_TEMPLATE_LABEL = 'PentAGI Penetration Testing Report (Default)';

// Free text on purpose (plan § 6.11): the language is handed to the model as
// words, not an IETF code. The suggestions are only a convenience, and leaving
// the field empty falls back to the flow's own language.
const LANGUAGE_SUGGESTIONS = ['English', 'Français', 'Deutsch', 'Español', 'Italiano', 'Português', 'Nederlands'];

const createFormSchema = z.object({
    additionalInstructions: z.string().max(4000),
    contentBudgetSeconds: z.string(),
    controlBudgetSeconds: z.string(),
    language: z.string().max(64),
    providerName: z.string(),
    templateKey: z.string(),
    title: z.string().max(256),
});

type CreateFormValues = z.infer<typeof createFormSchema>;

interface FlowReportCreateDialogProps {
    onCheckVision: (providerName: null | string) => Promise<{ error?: null | string; vision: boolean }>;
    onCreate: (input: CreateReportInput) => Promise<null | ReportProgress>;
    onCreated?: (report: ReportProgress) => void;
    onOpenChange: (open: boolean) => void;
    open: boolean;
}

function FlowReportCreateDialog({
    onCheckVision,
    onCreate,
    onCreated,
    onOpenChange,
    open,
}: FlowReportCreateDialogProps) {
    const { providers } = useProviders();
    const [isSubmitting, setIsSubmitting] = useState(false);
    const [visionWarning, setVisionWarning] = useState<null | { error?: null | string }>(null);

    // The template picker lists every enabled template the caller may use: the
    // bundled default plus their own and the global ones (backlog v2, L3).
    const { data: templatesData } = useQuery(ReportTemplatesDocument);
    const templates = useMemo(
        () => (templatesData?.reportTemplates ?? []).filter((template) => template.enabled),
        [templatesData],
    );

    const form = useForm<CreateFormValues>({
        defaultValues: {
            additionalInstructions: '',
            contentBudgetSeconds: '',
            controlBudgetSeconds: '',
            language: '',
            providerName: '',
            templateKey: DEFAULT_TEMPLATE_KEY,
            title: '',
        },
        resolver: zodResolver(createFormSchema),
    });

    const submitReport = async (values: CreateFormValues) => {
        setIsSubmitting(true);

        try {
            const budget = (value: string): null | number => {
                const parsed = Number.parseInt(value, 10);

                return Number.isFinite(parsed) && parsed > 0 ? parsed : null;
            };

            const report = await onCreate({
                additionalInstructions: values.additionalInstructions.trim() || null,
                contentBudgetSeconds: budget(values.contentBudgetSeconds),
                controlBudgetSeconds: budget(values.controlBudgetSeconds),
                language: values.language.trim() || null,
                providerName: values.providerName || null,
                templateKey: values.templateKey || null,
                title: values.title.trim() || null,
            });

            if (report) {
                onCreated?.(report);
                onOpenChange(false);
                form.reset();
                setVisionWarning(null);
            }
        } finally {
            setIsSubmitting(false);
        }
    };

    const handleSubmit = form.handleSubmit(async (values) => {
        // Probe the chosen model once, so a text-only one warns before the run
        // rather than silently dropping the screenshots (plan § 6.10, P7.4).
        if (!visionWarning) {
            setIsSubmitting(true);

            try {
                const vision = await onCheckVision(values.providerName || null);

                if (!vision.vision) {
                    setVisionWarning({ error: vision.error });

                    return;
                }
            } catch (error) {
                Log.error('Report vision probe failed:', error);
            } finally {
                setIsSubmitting(false);
            }
        }

        await submitReport(values);
    });

    return (
        <Dialog
            onOpenChange={onOpenChange}
            open={open}
        >
            <DialogContent className="sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>Create report</DialogTitle>
                    <DialogDescription>
                        The agent writes the document from what this engagement recorded. It runs for minutes and you
                        can watch it in this tab.
                    </DialogDescription>
                </DialogHeader>

                <Form {...form}>
                    <form
                        className="flex flex-col gap-4"
                        onSubmit={handleSubmit}
                    >
                        <FormField
                            control={form.control}
                            name="templateKey"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Template</FormLabel>
                                    <Select
                                        onValueChange={field.onChange}
                                        value={field.value || DEFAULT_TEMPLATE_KEY}
                                    >
                                        <SelectTrigger className="w-full">
                                            <SelectValue placeholder="Select a template" />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {templates.length === 0 && (
                                                <SelectItem value={DEFAULT_TEMPLATE_KEY}>
                                                    {DEFAULT_TEMPLATE_LABEL}
                                                </SelectItem>
                                            )}
                                            {templates.map((template) => (
                                                <SelectItem
                                                    key={template.key}
                                                    value={template.key}
                                                >
                                                    {template.label}
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="providerName"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>LLM provider</FormLabel>
                                    <Select
                                        onValueChange={(value) => {
                                            field.onChange(value);
                                            setVisionWarning(null);
                                        }}
                                        value={field.value || ''}
                                    >
                                        <SelectTrigger className="w-full">
                                            <SelectValue placeholder="The engagement's own" />
                                        </SelectTrigger>
                                        <SelectContent>
                                            <SelectItem value="">The engagement's own</SelectItem>
                                            {providers.map((provider) => (
                                                <SelectItem
                                                    key={provider.name}
                                                    value={provider.name}
                                                >
                                                    {provider.name}
                                                </SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="title"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Title</FormLabel>
                                    <FormControl>
                                        <Input
                                            {...field}
                                            placeholder="The engagement's own title"
                                            value={field.value ?? ''}
                                        />
                                    </FormControl>
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="language"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Language</FormLabel>
                                    <FormControl>
                                        <Input
                                            {...field}
                                            list="report-language-suggestions"
                                            placeholder="The engagement's own language"
                                            value={field.value ?? ''}
                                        />
                                    </FormControl>
                                    <datalist id="report-language-suggestions">
                                        {LANGUAGE_SUGGESTIONS.map((language) => (
                                            <option
                                                key={language}
                                                value={language}
                                            />
                                        ))}
                                    </datalist>
                                    <p className="text-muted-foreground text-xs">
                                        The agent writes the whole document (template and content) in that language.
                                        Empty keeps the engagement&apos;s own language, English by default.
                                    </p>
                                </FormItem>
                            )}
                        />

                        <FormField
                            control={form.control}
                            name="additionalInstructions"
                            render={({ field }) => (
                                <FormItem>
                                    <FormLabel>Additional instructions</FormLabel>
                                    <FormControl>
                                        <Textarea
                                            {...field}
                                            className="min-h-20 resize-none"
                                            placeholder="What this report should pay attention to. Every section is told."
                                            value={field.value ?? ''}
                                        />
                                    </FormControl>
                                </FormItem>
                            )}
                        />

                        <div className="grid grid-cols-2 gap-3">
                            <FormField
                                control={form.control}
                                name="controlBudgetSeconds"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>Control node budget (s)</FormLabel>
                                        <FormControl>
                                            <Input
                                                {...field}
                                                inputMode="numeric"
                                                placeholder="Default"
                                                value={field.value ?? ''}
                                            />
                                        </FormControl>
                                    </FormItem>
                                )}
                            />
                            <FormField
                                control={form.control}
                                name="contentBudgetSeconds"
                                render={({ field }) => (
                                    <FormItem>
                                        <FormLabel>Content node budget (s)</FormLabel>
                                        <FormControl>
                                            <Input
                                                {...field}
                                                inputMode="numeric"
                                                placeholder="Default"
                                                value={field.value ?? ''}
                                            />
                                        </FormControl>
                                    </FormItem>
                                )}
                            />
                        </div>

                        {visionWarning && (
                            <Alert variant="destructive">
                                <TriangleAlert />
                                <AlertTitle>This model cannot read images</AlertTitle>
                                <AlertDescription>
                                    Screenshots cited by the report will not be analysed.
                                    {visionWarning.error ? ` (${visionWarning.error})` : ''} Choose a vision-capable
                                    model, or create the report without image analysis.
                                </AlertDescription>
                            </Alert>
                        )}

                        <DialogFooter>
                            <Button
                                onClick={() => onOpenChange(false)}
                                type="button"
                                variant="ghost"
                            >
                                Cancel
                            </Button>
                            <FormSubmitButton
                                loading={isSubmitting}
                                requireValid={false}
                                type="submit"
                            >
                                <FilePlus2 className="size-4" />
                                {visionWarning ? 'Create report anyway' : 'Create report'}
                            </FormSubmitButton>
                        </DialogFooter>
                    </form>
                </Form>
            </DialogContent>
        </Dialog>
    );
}

export default FlowReportCreateDialog;
