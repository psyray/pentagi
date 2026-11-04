import { useMutation } from '@apollo/client/react';
import { ArrowLeft, Save } from 'lucide-react';
import { type FormEvent, useCallback, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { AppHeader, AppHeaderActions, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { ImportSkillDocument, SkillKind, SkillStatus, SkillVisibility } from '@/graphql/types';
import { routes } from '@/lib/routes';

function SkillForm() {
    const navigate = useNavigate();
    const [title, setTitle] = useState('');
    const [kind, setKind] = useState<SkillKind>(SkillKind.Skill);
    const [status, setStatus] = useState<SkillStatus>(SkillStatus.Draft);
    const [visibility, setVisibility] = useState<SkillVisibility>(SkillVisibility.Private);
    const [tags, setTags] = useState('');
    const [content, setContent] = useState('');

    const [importSkill, { loading }] = useMutation(ImportSkillDocument);

    const handleSubmit = useCallback(
        async (event: FormEvent<HTMLFormElement>) => {
            event.preventDefault();

            if (!content.trim()) {
                toast.error('The skill content is required');

                return;
            }

            try {
                const result = await importSkill({
                    variables: {
                        input: {
                            content,
                            kind,
                            status,
                            tags: tags
                                .split(',')
                                .map((tag) => tag.trim())
                                .filter(Boolean),
                            title: title.trim() || null,
                            visibility,
                        },
                    },
                });

                const slug = result.data?.importSkill.slug;

                toast.success('Skill imported');
                navigate(slug ? routes.skill(slug) : routes.skills);
            } catch {
                toast.error('Unable to import the skill');
            }
        },
        [content, importSkill, kind, navigate, status, tags, title, visibility],
    );

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <Button
                        onClick={() => navigate(routes.skills)}
                        size="sm"
                        variant="ghost"
                    >
                        <ArrowLeft />
                        Skills
                    </Button>
                    <AppHeaderTitle>New skill</AppHeaderTitle>
                </AppHeaderContent>
                <AppHeaderActions>
                    <Button
                        disabled={loading}
                        form="skill-form"
                        size="sm"
                        type="submit"
                    >
                        <Save />
                        Import
                    </Button>
                </AppHeaderActions>
            </AppHeader>
            <div className="flex flex-1 flex-col overflow-auto p-4">
                <form
                    className="mx-auto flex w-full max-w-3xl flex-col gap-4"
                    id="skill-form"
                    onSubmit={handleSubmit}
                >
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="skill-title">Title</Label>
                        <Input
                            id="skill-title"
                            onChange={(event) => setTitle(event.target.value)}
                            placeholder="Optional when the markdown starts with a level-one heading"
                            value={title}
                        />
                    </div>

                    <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="skill-kind">Kind</Label>
                            <Select
                                onValueChange={(value) => setKind(value as SkillKind)}
                                value={kind}
                            >
                                <SelectTrigger
                                    aria-label="Skill kind"
                                    id="skill-kind"
                                >
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value={SkillKind.Skill}>Skill</SelectItem>
                                    <SelectItem value={SkillKind.Reference}>Reference</SelectItem>
                                    <SelectItem value={SkillKind.Mission}>Mission</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="skill-status">Status</Label>
                            <Select
                                onValueChange={(value) => setStatus(value as SkillStatus)}
                                value={status}
                            >
                                <SelectTrigger
                                    aria-label="Skill status"
                                    id="skill-status"
                                >
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value={SkillStatus.Draft}>Draft</SelectItem>
                                    <SelectItem value={SkillStatus.Reviewed}>Reviewed</SelectItem>
                                    <SelectItem value={SkillStatus.Canonical}>Canonical</SelectItem>
                                    <SelectItem value={SkillStatus.Deprecated}>Deprecated</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>

                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="skill-visibility">Visibility</Label>
                            <Select
                                onValueChange={(value) => setVisibility(value as SkillVisibility)}
                                value={visibility}
                            >
                                <SelectTrigger
                                    aria-label="Skill visibility"
                                    id="skill-visibility"
                                >
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value={SkillVisibility.Private}>Private</SelectItem>
                                    <SelectItem value={SkillVisibility.Shared}>Shared</SelectItem>
                                </SelectContent>
                            </Select>
                        </div>
                    </div>

                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="skill-tags">Tags</Label>
                        <Input
                            id="skill-tags"
                            onChange={(event) => setTags(event.target.value)}
                            placeholder="adcs, ntlm, esc8"
                            value={tags}
                        />
                        <p className="text-muted-foreground text-xs">Comma-separated. Used by the skill filter.</p>
                    </div>

                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="skill-content">Markdown</Label>
                        <Textarea
                            className="min-h-[420px] font-mono text-xs"
                            id="skill-content"
                            onChange={(event) => setContent(event.target.value)}
                            placeholder={'# Skill title\n\n## When to use\n\n...\n\n## Procedure\n\n...'}
                            value={content}
                        />
                        <p className="text-muted-foreground text-xs">
                            YAML frontmatter is optional. The title comes from the frontmatter, the form, or the first
                            heading, in that order.
                        </p>
                    </div>
                </form>
            </div>
        </>
    );
}

export default SkillForm;
