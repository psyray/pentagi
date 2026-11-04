import { Download, Paperclip, Trash, Upload } from 'lucide-react';
import { type ChangeEvent, useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Spinner } from '@/components/ui/spinner';
import { api, getApiErrorMessage, unwrapApiResponse } from '@/lib/axios';
import { baseUrl } from '@/models/api';

interface SkillFile {
    blobRef: string;
    mime: string;
    path: string;
    sha256: string;
    size: number;
}

interface SkillFilesProps {
    skillSlug: string;
}

const filesPath = (slug: string) => `/skills/${encodeURIComponent(slug)}/files`;

const filePath = (slug: string, path: string) =>
    `${filesPath(slug)}/${path.split('/').map(encodeURIComponent).join('/')}`;

export function SkillFiles({ skillSlug }: SkillFilesProps) {
    const [files, setFiles] = useState<SkillFile[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isUploading, setIsUploading] = useState(false);
    const [deleting, setDeleting] = useState<null | string>(null);
    const inputRef = useRef<HTMLInputElement>(null);

    const load = useCallback(async () => {
        try {
            const response = await api.get<{ files: SkillFile[]; total: number }>(filesPath(skillSlug));
            const data = unwrapApiResponse(response);

            setFiles(data.files ?? []);
        } catch (error) {
            toast.error(getApiErrorMessage(error, 'Unable to load the attachments'));
        } finally {
            setIsLoading(false);
        }
    }, [skillSlug]);

    useEffect(() => {
        let cancelled = false;

        api.get<{ files: SkillFile[]; total: number }>(filesPath(skillSlug))
            .then((response) => {
                if (!cancelled) {
                    const data = unwrapApiResponse(response);

                    setFiles(data.files ?? []);
                }
            })
            .catch((error: unknown) => {
                if (!cancelled) {
                    toast.error(getApiErrorMessage(error, 'Unable to load the attachments'));
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

    const handleUpload = useCallback(
        async (event: ChangeEvent<HTMLInputElement>) => {
            const selected = event.target.files ? Array.from(event.target.files) : [];

            event.target.value = '';

            if (selected.length === 0) {
                return;
            }

            setIsUploading(true);

            try {
                const form = new FormData();

                for (const file of selected) {
                    form.append('files', file);
                }

                const response = await api.post(filesPath(skillSlug), form);

                unwrapApiResponse(response);
                toast.success(selected.length === 1 ? 'File uploaded' : `${selected.length} files uploaded`);
                await load();
            } catch (error) {
                toast.error(getApiErrorMessage(error, 'Unable to upload the file'));
            } finally {
                setIsUploading(false);
            }
        },
        [load, skillSlug],
    );

    const handleDelete = useCallback(
        async (path: string) => {
            setDeleting(path);

            try {
                const response = await api.delete(filePath(skillSlug, path));

                unwrapApiResponse(response);
                toast.success('File deleted');
                await load();
            } catch (error) {
                toast.error(getApiErrorMessage(error, 'Unable to delete the file'));
            } finally {
                setDeleting(null);
            }
        },
        [load, skillSlug],
    );

    return (
        <section className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
                <h2 className="flex items-center gap-1.5 text-sm font-medium">
                    <Paperclip className="size-4" />
                    Attachments
                </h2>
                <Button
                    disabled={isUploading}
                    onClick={() => inputRef.current?.click()}
                    size="sm"
                    variant="secondary"
                >
                    {isUploading ? <Spinner variant="circle" /> : <Upload />}
                    Add file
                </Button>
                <input
                    className="hidden"
                    multiple
                    onChange={handleUpload}
                    ref={inputRef}
                    type="file"
                />
            </div>
            {isLoading ? (
                <p className="text-muted-foreground text-xs">Loading attachments...</p>
            ) : files.length === 0 ? (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <Paperclip />
                        </EmptyMedia>
                        <EmptyTitle>No attachments</EmptyTitle>
                        <EmptyDescription>
                            Add scripts, wordlists or configuration files to this skill.
                        </EmptyDescription>
                    </EmptyHeader>
                </Empty>
            ) : (
                <ul className="flex flex-col divide-y rounded-lg border">
                    {files.map((file) => (
                        <li
                            className="flex items-center gap-3 px-3 py-2"
                            key={file.path}
                        >
                            <div className="min-w-0 flex-1">
                                <p className="truncate font-mono text-xs">{file.path}</p>
                                <p className="text-muted-foreground text-xs">
                                    {formatSize(file.size)}
                                    {file.mime ? `, ${file.mime}` : ''}
                                </p>
                            </div>
                            <Button
                                aria-label={`Download ${file.path}`}
                                asChild
                                size="sm"
                                variant="ghost"
                            >
                                <a
                                    download
                                    href={`${baseUrl}${filePath(skillSlug, file.path)}`}
                                >
                                    <Download />
                                </a>
                            </Button>
                            <Button
                                aria-label={`Delete ${file.path}`}
                                disabled={deleting === file.path}
                                onClick={() => void handleDelete(file.path)}
                                size="sm"
                                variant="ghost"
                            >
                                {deleting === file.path ? <Spinner variant="circle" /> : <Trash />}
                            </Button>
                        </li>
                    ))}
                </ul>
            )}
        </section>
    );
}

function formatSize(bytes: number): string {
    if (bytes < 1024) {
        return `${bytes} B`;
    }

    if (bytes < 1024 * 1024) {
        return `${(bytes / 1024).toFixed(1)} KiB`;
    }

    return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}
