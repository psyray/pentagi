// Report-template branding logo (backlog v2, L3). The API serves the stored
// logo with the session cookie, so the Settings editor renders it in an <img>;
// it never travels through GraphQL (unlike the four text fields).

import { baseUrl } from '@/models/api';

// Relative to the axios base URL, for a future download.
export const reportTemplateLogoPath = (templateId: null | number | string): string =>
    `/report-templates/${templateId}/branding/logo`;

// Absolute, for an <img> that cannot go through axios (the API base has to be
// part of the URL, or the static handler answers the SPA shell instead). The
// `cacheKey` busts the browser cache after an upload: the URL itself never
// changes.
export const reportTemplateLogoUrl = (templateId: null | number | string, cacheKey?: number | string): string =>
    `${baseUrl}${reportTemplateLogoPath(templateId)}${cacheKey === undefined ? '' : `?v=${cacheKey}`}`;
