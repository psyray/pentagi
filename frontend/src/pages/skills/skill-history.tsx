import { History } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';

import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { api, getApiErrorMessage, unwrapApiResponse } from '@/lib/axios';
import { formatDate } from '@/lib/utils/format';

import { skillStatusBadgeVariant } from './skill-badges';
import { type DiffLineKind, parseUnifiedDiff } from './skill-history-diff';

interface SkillHistoryProps {
    skillSlug: string;
}

interface SkillVersion {
    Chars: number;
    ContentHash: string;
    CreatedAt: string;
    Status: string;
    Title: string;
    Version: number;
}

interface SkillVersionDiff {
    Added: number;
    Changed: boolean;
    Diff: string;
    From: number;
    Removed: number;
    To: number;
}

const versionsPath = (slug: string) => `/skills/${encodeURIComponent(slug)}/versions`;

const diffPath = (slug: string, from: number, to: number) =>
    `/skills/${encodeURIComponent(slug)}/diff?from=${from}&to=${to}`;

const statusVariant = (status: string): BadgeVariant =>
    (skillStatusBadgeVariant as Record<string, BadgeVariant>)[status.toLowerCase()] ?? 'outline';

export function SkillHistory({ skillSlug }: SkillHistoryProps) {
    const [versions, setVersions] = useState<SkillVersion[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [fromVersion, setFromVersion] = useState<null | number>(null);
    const [toVersion, setToVersion] = useState<null | number>(null);
    const [isLoadingDiff, setIsLoadingDiff] = useState(false);
    const [isDiffOpen, setIsDiffOpen] = useState(false);
    const [diff, setDiff] = useState<null | SkillVersionDiff>(null);

    useEffect(() => {
        let cancelled = false;

        api.get<{ total: number; versions: SkillVersion[] }>(versionsPath(skillSlug))
            .then((response) => {
                if (cancelled) {
                    return;
                }

                const list = unwrapApiResponse(response).versions ?? [];

                setVersions(list);
                setToVersion(list[0]?.Version ?? null);
                setFromVersion(list[1]?.Version ?? list[0]?.Version ?? null);
            })
            .catch((error: unknown) => {
                if (!cancelled) {
                    toast.error(getApiErrorMessage(error, 'Unable to load the history'));
                }
            })
            .finally(() => {
                if (!cancelled) {
                    setIsLoading(false);
                }
            });

        return () => {
            cancelled = true;
        };
    }, [skillSlug]);

    const handleCompare = useCallback(async () => {
        if (fromVersion === null || toVersion === null) {
            return;
        }

        setIsLoadingDiff(true);

        try {
            const response = await api.get<SkillVersionDiff>(diffPath(skillSlug, fromVersion, toVersion));
            const result = unwrapApiResponse(response);

            if (!result.Changed) {
                toast.info(`v${result.From} and v${result.To} are identical`);

                return;
            }

            setDiff(result);
            setIsDiffOpen(true);
        } catch (error) {
            toast.error(getApiErrorMessage(error, 'Unable to compare the versions'));
        } finally {
            setIsLoadingDiff(false);
        }
    }, [fromVersion, skillSlug, toVersion]);

    const lines = diff ? parseUnifiedDiff(diff.Diff) : [];
    const canCompare = versions.length > 1 && fromVersion !== null && toVersion !== null;

    return (
        <section className="flex flex-col gap-3">
            <div className="flex flex-wrap items-center gap-2">
                <h2 className="flex items-center gap-1.5 text-sm font-medium">
                    <History className="size-4" />
                    History
                </h2>
                {versions.length > 1 ? (
                    <div className="ml-auto flex flex-wrap items-center gap-2">
                        <Select
                            onValueChange={(value) => setFromVersion(Number(value))}
                            value={fromVersion === null ? undefined : String(fromVersion)}
                        >
                            <SelectTrigger
                                aria-label="Source version"
                                className="h-8 w-28"
                            >
                                <SelectValue placeholder="From" />
                            </SelectTrigger>
                            <SelectContent>
                                {versions.map((version) => (
                                    <SelectItem
                                        key={version.Version}
                                        value={String(version.Version)}
                                    >
                                        v{version.Version}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <span className="text-muted-foreground text-xs">to</span>
                        <Select
                            onValueChange={(value) => setToVersion(Number(value))}
                            value={toVersion === null ? undefined : String(toVersion)}
                        >
                            <SelectTrigger
                                aria-label="Target version"
                                className="h-8 w-28"
                            >
                                <SelectValue placeholder="To" />
                            </SelectTrigger>
                            <SelectContent>
                                {versions.map((version) => (
                                    <SelectItem
                                        key={version.Version}
                                        value={String(version.Version)}
                                    >
                                        v{version.Version}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <Button
                            disabled={!canCompare || isLoadingDiff}
                            onClick={() => void handleCompare()}
                            size="sm"
                            variant="secondary"
                        >
                            {isLoadingDiff ? <Spinner variant="circle" /> : <History />}
                            Show diff
                        </Button>
                    </div>
                ) : null}
            </div>

            {isLoading ? (
                <p className="text-muted-foreground text-xs">Loading history...</p>
            ) : versions.length === 0 ? (
                <p className="text-muted-foreground text-xs">No stored version yet.</p>
            ) : (
                <ul className="flex flex-col divide-y rounded-lg border">
                    {versions.map((version) => (
                        <li
                            className="flex flex-wrap items-center gap-3 px-3 py-2"
                            key={version.Version}
                        >
                            <span className="font-mono text-xs">v{version.Version}</span>
                            <Badge variant={statusVariant(version.Status)}>{version.Status}</Badge>
                            <span className="text-muted-foreground text-xs">
                                {version.Chars} chars, {formatDate(new Date(version.CreatedAt))}
                            </span>
                            <span className="text-muted-foreground ml-auto font-mono text-xs">
                                {version.ContentHash.slice(0, 12)}
                            </span>
                        </li>
                    ))}
                </ul>
            )}

            {!isLoading && versions.length === 1 ? (
                <p className="text-muted-foreground text-xs">
                    A new version is stored each time the document is imported.
                </p>
            ) : null}

            <Dialog
                onOpenChange={setIsDiffOpen}
                open={isDiffOpen}
            >
                <DialogContent className="sm:max-w-3xl">
                    <DialogHeader>
                        <DialogTitle>
                            v{diff?.From} to v{diff?.To}
                        </DialogTitle>
                        <DialogDescription>
                            {diff ? `+${diff.Added} additions, -${diff.Removed} removals` : null}
                        </DialogDescription>
                    </DialogHeader>
                    <pre className="bg-muted/40 max-h-[60vh] overflow-auto rounded-md border p-3 font-mono text-xs leading-relaxed">
                        {lines.map((line, index) => (
                            <div
                                className={lineClassName(line.kind)}
                                key={`${index}-${line.text}`}
                            >
                                {line.text || ' '}
                            </div>
                        ))}
                    </pre>
                </DialogContent>
            </Dialog>
        </section>
    );
}

function lineClassName(kind: DiffLineKind): string | undefined {
    switch (kind) {
        case 'add':
            return 'text-emerald-600 dark:text-emerald-400';
        case 'header':
            return 'text-muted-foreground font-medium';
        case 'hunk':
            return 'text-muted-foreground';
        case 'remove':
            return 'text-red-600 dark:text-red-400';
        default:
            return undefined;
    }
}
