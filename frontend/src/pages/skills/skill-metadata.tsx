import { useMutation } from '@apollo/client/react';
import { Save } from 'lucide-react';
import { useCallback, useState } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { type SkillFragmentFragment, SkillStatus, SkillVisibility, UpdateSkillDocument } from '@/graphql/types';

interface SkillMetadataProps {
    skill: SkillFragmentFragment;
}

export function SkillMetadata({ skill }: SkillMetadataProps) {
    const [status, setStatus] = useState<SkillStatus>(skill.status);
    const [visibility, setVisibility] = useState<SkillVisibility>(skill.visibility);
    const [tags, setTags] = useState(skill.tags.join(', '));
    const [description, setDescription] = useState(skill.description);

    const [updateSkill, { loading }] = useMutation(UpdateSkillDocument, {
        refetchQueries: ['skill', 'skills', 'searchSkills'],
    });

    const handleSave = useCallback(async () => {
        try {
            await updateSkill({
                variables: {
                    idOrSlug: skill.slug,
                    input: {
                        description,
                        status,
                        tags: tags
                            .split(',')
                            .map((tag) => tag.trim())
                            .filter(Boolean),
                        visibility,
                    },
                },
            });

            toast.success('Skill metadata updated');
        } catch {
            toast.error('Unable to update the skill');
        }
    }, [description, skill.slug, status, tags, updateSkill, visibility]);

    return (
        <section className="flex flex-col gap-3">
            <h2 className="text-sm font-medium">Metadata</h2>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="skill-edit-status">Status</Label>
                    <Select
                        onValueChange={(value) => setStatus(value as SkillStatus)}
                        value={status}
                    >
                        <SelectTrigger
                            aria-label="Skill status"
                            id="skill-edit-status"
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
                    <Label htmlFor="skill-edit-visibility">Visibility</Label>
                    <Select
                        onValueChange={(value) => setVisibility(value as SkillVisibility)}
                        value={visibility}
                    >
                        <SelectTrigger
                            aria-label="Skill visibility"
                            id="skill-edit-visibility"
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
                <Label htmlFor="skill-edit-tags">Tags</Label>
                <Input
                    id="skill-edit-tags"
                    onChange={(event) => setTags(event.target.value)}
                    placeholder="adcs, ntlm, esc8"
                    value={tags}
                />
            </div>

            <div className="flex flex-col gap-1.5">
                <Label htmlFor="skill-edit-description">Description</Label>
                <Textarea
                    className="min-h-20"
                    id="skill-edit-description"
                    onChange={(event) => setDescription(event.target.value)}
                    value={description}
                />
            </div>

            <div>
                <Button
                    disabled={loading}
                    onClick={() => void handleSave()}
                    size="sm"
                >
                    <Save />
                    Save metadata
                </Button>
            </div>
        </section>
    );
}
