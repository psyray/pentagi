import type { BadgeVariant } from '@/components/ui/badge';

import { SkillKind, SkillStatus } from '@/graphql/types';

export const skillKindBadgeVariant: Record<SkillKind, BadgeVariant> = {
    [SkillKind.Mission]: 'purple',
    [SkillKind.Reference]: 'blue',
    [SkillKind.Skill]: 'green',
};

export const skillStatusBadgeVariant: Record<SkillStatus, BadgeVariant> = {
    [SkillStatus.Canonical]: 'green',
    [SkillStatus.Deprecated]: 'red',
    [SkillStatus.Draft]: 'outline',
    [SkillStatus.Reviewed]: 'secondary',
};
