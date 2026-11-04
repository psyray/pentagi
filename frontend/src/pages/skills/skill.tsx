import { skipToken, useMutation, useQuery } from '@apollo/client/react';
import { ArrowLeft, Trash } from 'lucide-react';
import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { toast } from 'sonner';

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
import Markdown from '@/components/shared/markdown';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Separator } from '@/components/ui/separator';
import { DeleteSkillDocument, SkillDocument } from '@/graphql/types';
import { isNotFoundError } from '@/lib/errors';
import { routes } from '@/lib/routes';
import { formatDate } from '@/lib/utils/format';

import { skillKindBadgeVariant, skillStatusBadgeVariant } from './skill-badges';
import { SkillFiles } from './skill-files';
import { SkillMetadata } from './skill-metadata';

function Skill() {
    const navigate = useNavigate();
    const { skillId } = useParams<{ skillId?: string }>();
    const [isDeleteOpen, setIsDeleteOpen] = useState(false);

    const shouldFetch = Boolean(skillId);

    const { data, error, loading, refetch } = useQuery(
        SkillDocument,
        shouldFetch && skillId ? { variables: { idOrSlug: skillId } } : skipToken,
    );

    const [deleteSkill] = useMutation(DeleteSkillDocument, {
        refetchQueries: ['skills', 'searchSkills'],
    });

    const skill = data?.skill ?? null;
    const loadError = error && !skill && !isNotFoundError(error) ? error : undefined;

    useEffect(() => {
        if (loading || loadError) {
            return;
        }

        if (!skill) {
            toast.error('Skill not found');
            navigate(routes.skills, { replace: true });
        }
    }, [loading, loadError, navigate, skill]);

    const handleDelete = useCallback(async () => {
        if (!skill) {
            return;
        }

        try {
            await deleteSkill({ variables: { idOrSlug: skill.slug } });
            toast.success('Skill deleted');
            navigate(routes.skills, { replace: true });
        } catch {
            toast.error('Unable to delete the skill');
        }
    }, [deleteSkill, navigate, skill]);

    const header = (
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
                <AppHeaderTitle>{skill?.title ?? 'Skill'}</AppHeaderTitle>
            </AppHeaderContent>
            <AppHeaderActions>
                <AppHeaderAction
                    disabled={!skill}
                    icon={<Trash />}
                    label="Delete"
                    onClick={() => setIsDeleteOpen(true)}
                    variant="destructive"
                />
            </AppHeaderActions>
        </AppHeader>
    );

    if (loading && !skill) {
        return (
            <>
                {header}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <LoadingState
                        description="Please wait while we fetch the skill"
                        title="Loading skill..."
                    />
                </div>
            </>
        );
    }

    if (loadError && !skill) {
        return (
            <>
                {header}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <ErrorState
                        message={loadError.message}
                        onRetry={refetch}
                        title="Error loading skill"
                    />
                </div>
            </>
        );
    }

    if (!skill) {
        return null;
    }

    return (
        <>
            {header}
            <div className="flex flex-1 flex-col gap-4 overflow-auto p-4">
                <div className="flex flex-col gap-2">
                    <div className="flex flex-wrap items-center gap-2">
                        <Badge variant={skillKindBadgeVariant[skill.kind]}>{skill.kind}</Badge>
                        <Badge variant={skillStatusBadgeVariant[skill.status]}>{skill.status}</Badge>
                        <Badge variant="outline">{skill.visibility}</Badge>
                        <span className="text-muted-foreground text-xs">v{skill.version}</span>
                        {skill.language ? (
                            <span className="text-muted-foreground text-xs">{skill.language}</span>
                        ) : null}
                    </div>
                    {skill.description ? <p className="text-muted-foreground text-sm">{skill.description}</p> : null}
                    {skill.tags.length > 0 ? (
                        <div className="flex flex-wrap gap-1">
                            {skill.tags.map((tag) => (
                                <Badge
                                    key={tag}
                                    variant="outline"
                                >
                                    {tag}
                                </Badge>
                            ))}
                        </div>
                    ) : null}
                </div>

                <Separator />

                <Markdown>{skill.content}</Markdown>

                <Separator />

                <SkillMetadata skill={skill} />

                <Separator />

                <SkillFiles
                    key={skill.slug}
                    skillSlug={skill.slug}
                />

                <Separator />

                <dl className="text-muted-foreground grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-xs">
                    <dt>Slug</dt>
                    <dd className="truncate font-mono">{skill.slug}</dd>
                    <dt>Source</dt>
                    <dd className="truncate">
                        {skill.sourceKind}
                        {skill.sourceUri ? `, ${skill.sourceUri}` : ''}
                    </dd>
                    <dt>Updated</dt>
                    <dd>{formatDate(new Date(skill.updatedAt))}</dd>
                </dl>
            </div>
            <ConfirmationDialog
                cancelText="Cancel"
                confirmText="Delete"
                handleConfirm={handleDelete}
                handleOpenChange={setIsDeleteOpen}
                isOpen={isDeleteOpen}
                itemName={skill.title}
                itemType="skill"
            />
        </>
    );
}

export default Skill;
