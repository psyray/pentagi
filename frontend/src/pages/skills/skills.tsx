import { useMutation, useQuery } from '@apollo/client/react';
import { BookOpenCheck, Plus, Trash } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { useDebounce } from 'use-debounce';

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
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { InputSearch } from '@/components/ui/input-search';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
    DeleteSkillDocument,
    SearchSkillsDocument,
    type SkillFragmentFragment,
    SkillKind,
    SkillsDocument,
    SkillStatus,
} from '@/graphql/types';
import { routes } from '@/lib/routes';

import { skillKindBadgeVariant, skillStatusBadgeVariant } from './skill-badges';

const ALL = 'all';

function Skills() {
    const navigate = useNavigate();
    const [search, setSearch] = useState('');
    const [debouncedSearch] = useDebounce(search, 400);
    const [kind, setKind] = useState<SkillKind | typeof ALL>(ALL);
    const [status, setStatus] = useState<SkillStatus | typeof ALL>(ALL);
    const [deleting, setDeleting] = useState<null | SkillFragmentFragment>(null);
    const [isDeleteOpen, setIsDeleteOpen] = useState(false);

    const inSearchMode = debouncedSearch.trim().length > 0;

    const filter = useMemo(
        () => ({
            kinds: kind === ALL ? null : [kind],
            statuses: status === ALL ? null : [status],
        }),
        [kind, status],
    );

    const listQuery = useQuery(SkillsDocument, {
        fetchPolicy: 'cache-and-network',
        skip: inSearchMode,
        variables: { filter, limit: 200 },
    });

    const searchQuery = useQuery(SearchSkillsDocument, {
        fetchPolicy: 'cache-and-network',
        skip: !inSearchMode,
        variables: { filter, limit: 50, query: debouncedSearch },
    });

    const [deleteSkill] = useMutation(DeleteSkillDocument, {
        refetchQueries: ['skills', 'searchSkills'],
    });

    const skills: SkillFragmentFragment[] = inSearchMode
        ? (searchQuery.data?.searchSkills ?? []).map((hit) => hit.document)
        : (listQuery.data?.skills ?? []);

    const excerpts = useMemo(() => {
        const map = new Map<string, string>();

        for (const hit of searchQuery.data?.searchSkills ?? []) {
            map.set(hit.document.id, hit.excerpt);
        }

        return map;
    }, [searchQuery.data]);

    const isLoading = inSearchMode ? searchQuery.loading : listQuery.loading;
    const error = inSearchMode ? searchQuery.error : listQuery.error;
    const refetch = inSearchMode ? searchQuery.refetch : listQuery.refetch;

    const handleDelete = useCallback(async () => {
        if (!deleting) {
            return;
        }

        try {
            await deleteSkill({ variables: { idOrSlug: deleting.slug } });
            toast.success('Skill deleted');
            setDeleting(null);
        } catch {
            toast.error('Unable to delete the skill');
        }
    }, [deleteSkill, deleting]);

    const header = (
        <AppHeader>
            <AppHeaderContent>
                <AppHeaderTitle icon={<BookOpenCheck className="size-4 shrink-0" />}>Skills</AppHeaderTitle>
            </AppHeaderContent>
            <AppHeaderActions>
                <InputSearch
                    ariaLabel="Search skills"
                    hotkey="k"
                    maxWidth={240}
                    onSearchChange={setSearch}
                    placeholder="Semantic search..."
                    searchQuery={search}
                />
                <Select
                    onValueChange={(value) => setKind(value === ALL ? ALL : (value as SkillKind))}
                    value={kind}
                >
                    <SelectTrigger
                        aria-label="Filter by kind"
                        className="h-8 w-32"
                    >
                        <SelectValue placeholder="Kind" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>All kinds</SelectItem>
                        <SelectItem value={SkillKind.Skill}>Skill</SelectItem>
                        <SelectItem value={SkillKind.Reference}>Reference</SelectItem>
                        <SelectItem value={SkillKind.Mission}>Mission</SelectItem>
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => setStatus(value === ALL ? ALL : (value as SkillStatus))}
                    value={status}
                >
                    <SelectTrigger
                        aria-label="Filter by status"
                        className="h-8 w-32"
                    >
                        <SelectValue placeholder="Status" />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>All statuses</SelectItem>
                        <SelectItem value={SkillStatus.Draft}>Draft</SelectItem>
                        <SelectItem value={SkillStatus.Reviewed}>Reviewed</SelectItem>
                        <SelectItem value={SkillStatus.Canonical}>Canonical</SelectItem>
                        <SelectItem value={SkillStatus.Deprecated}>Deprecated</SelectItem>
                    </SelectContent>
                </Select>
                <AppHeaderAction
                    icon={<Plus />}
                    label="New skill"
                    onClick={() => navigate(routes.newSkill)}
                    variant="secondary"
                />
            </AppHeaderActions>
        </AppHeader>
    );

    if (isLoading && skills.length === 0) {
        return (
            <>
                {header}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <LoadingState
                        description="Please wait while we fetch your skills"
                        title="Loading skills..."
                    />
                </div>
            </>
        );
    }

    if (error && skills.length === 0) {
        return (
            <>
                {header}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <ErrorState
                        message={error.message}
                        onRetry={refetch}
                        title="Error loading skills"
                    />
                </div>
            </>
        );
    }

    return (
        <>
            {header}
            <div className="flex flex-1 flex-col gap-3 p-4">
                {skills.length === 0 ? (
                    <Empty>
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <BookOpenCheck />
                            </EmptyMedia>
                            <EmptyTitle>No skills found</EmptyTitle>
                            <EmptyDescription>
                                {inSearchMode
                                    ? 'No skill matches this search.'
                                    : 'Import a markdown skill or reference to get started.'}
                            </EmptyDescription>
                        </EmptyHeader>
                        <EmptyContent>
                            <Button
                                onClick={() => navigate(routes.newSkill)}
                                variant="secondary"
                            >
                                <Plus />
                                New skill
                            </Button>
                        </EmptyContent>
                    </Empty>
                ) : (
                    skills.map((skill) => (
                        <div
                            className="group hover:bg-accent/50 flex cursor-pointer flex-col gap-2 rounded-lg border p-3 transition-colors"
                            key={skill.id}
                            onClick={() => navigate(routes.skill(skill.slug))}
                            onKeyDown={(event) => {
                                if (event.key === 'Enter' || event.key === ' ') {
                                    event.preventDefault();
                                    navigate(routes.skill(skill.slug));
                                }
                            }}
                            role="button"
                            tabIndex={0}
                        >
                            <div className="flex flex-wrap items-center gap-2">
                                <Badge variant={skillKindBadgeVariant[skill.kind]}>{skill.kind}</Badge>
                                <Badge variant={skillStatusBadgeVariant[skill.status]}>{skill.status}</Badge>
                                <span className="font-medium">{skill.title}</span>
                                <span className="text-muted-foreground text-xs">v{skill.version}</span>
                                <Button
                                    aria-label="Delete skill"
                                    className="ml-auto size-8 p-0 opacity-0 transition-opacity group-hover:opacity-100"
                                    onClick={(event) => {
                                        event.stopPropagation();
                                        setDeleting(skill);
                                        setIsDeleteOpen(true);
                                    }}
                                    variant="ghost"
                                >
                                    <Trash />
                                </Button>
                            </div>
                            {skill.description ? (
                                <p className="text-muted-foreground line-clamp-2 text-sm">{skill.description}</p>
                            ) : null}
                            {excerpts.get(skill.id) ? (
                                <p className="text-muted-foreground bg-muted/50 line-clamp-2 rounded p-2 text-xs">
                                    {excerpts.get(skill.id)}
                                </p>
                            ) : null}
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
                    ))
                )}
            </div>
            <ConfirmationDialog
                cancelText="Cancel"
                confirmText="Delete"
                handleConfirm={handleDelete}
                handleOpenChange={setIsDeleteOpen}
                isOpen={isDeleteOpen}
                itemName={deleting?.title}
                itemType="skill"
            />
        </>
    );
}

export default Skills;
