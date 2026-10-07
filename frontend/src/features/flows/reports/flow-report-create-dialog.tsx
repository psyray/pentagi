import { zodResolver } from '@hookform/resolvers/zod';
import { FilePlus2 } from 'lucide-react';
import { useState } from 'react';
import { useForm } from 'react-hook-form';
import { z } from 'zod';

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
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { useProviders } from '@/providers/providers-provider';

import { type CreateReportInput, type ReportProgress } from './use-flow-reports';

const DEFAULT_TEMPLATE_KEY = 'pentagi-default-pentest-report';
const DEFAULT_TEMPLATE_LABEL = 'PentAGI Penetration Testing Report (Default)';

const createFormSchema = z.object({
    additionalInstructions: z.string().max(4000),
    contentBudgetSeconds: z.string(),
    controlBudgetSeconds: z.string(),
    providerName: z.string(),
    templateKey: z.string(),
    title: z.string().max(256),
});

type CreateFormValues = z.infer<typeof createFormSchema>;

interface FlowReportCreateDialogProps {
    onCreate: (input: CreateReportInput) => Promise<null | ReportProgress>;
    onCreated?: (report: ReportProgress) => void;
    onOpenChange: (open: boolean) => void;
    open: boolean;
}

function FlowReportCreateDialog({ onCreate, onCreated, onOpenChange, open }: FlowReportCreateDialogProps) {
    const { providers } = useProviders();
    const [isSubmitting, setIsSubmitting] = useState(false);

    const form = useForm<CreateFormValues>({
        defaultValues: {
            additionalInstructions: '',
            contentBudgetSeconds: '',
            controlBudgetSeconds: '',
            providerName: '',
            templateKey: DEFAULT_TEMPLATE_KEY,
            title: '',
        },
        resolver: zodResolver(createFormSchema),
    });

    const handleSubmit = form.handleSubmit(async (values) => {
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
                providerName: values.providerName || null,
                templateKey: values.templateKey || null,
                title: values.title.trim() || null,
            });

            if (report) {
                onCreated?.(report);
                onOpenChange(false);
                form.reset();
            }
        } finally {
            setIsSubmitting(false);
        }
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
                                            <SelectItem value={DEFAULT_TEMPLATE_KEY}>
                                                {DEFAULT_TEMPLATE_LABEL}
                                            </SelectItem>
                                            {/* User-defined report templates land with the
                                                settings interface (plan § 6.9). */}
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
                                        onValueChange={field.onChange}
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

                        <DialogFooter>
                            <Button
                                onClick={() => onOpenChange(false)}
                                type="button"
                                variant="ghost"
                            >
                                Cancel
                            </Button>
                            <Button
                                loading={isSubmitting}
                                type="submit"
                            >
                                <FilePlus2 className="size-4" />
                                Create report
                            </Button>
                        </DialogFooter>
                    </form>
                </Form>
            </DialogContent>
        </Dialog>
    );
}

export default FlowReportCreateDialog;
