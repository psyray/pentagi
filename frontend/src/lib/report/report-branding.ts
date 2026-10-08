// Branding plumbing shared by the document renderers (plan § 6.6 / P6). The
// server resolves [[title]] and [[date]]; the page tokens belong here, because
// only a page renderer knows the page count.

export type BrandingToken = { kind: 'page' | 'pages' | 'text'; value: string };

// splitPageTokens keeps the page-number placeholders apart so a Word run can
// carry a real PageNumber field, while a plain-text renderer just substitutes.
export const splitPageTokens = (text: string): BrandingToken[] =>
    text
        .split(/(\[\[page\]\]|\[\[pages\]\])/)
        .filter((part) => part !== '')
        .map((part) => {
            if (part === '[[page]]') {
                return { kind: 'page' as const, value: '' };
            }

            if (part === '[[pages]]') {
                return { kind: 'pages' as const, value: '' };
            }

            return { kind: 'text' as const, value: part };
        });

export const brandingHasPageTokens = (text: string): boolean => /\[\[(page|pages)\]\]/.test(text);

export const resolvePageTokens = (text: string, page: number, pages: number): string =>
    text.replaceAll('[[page]]', String(page)).replaceAll('[[pages]]', String(pages));

// scaleImage keeps an image's aspect inside a maximum width, for the renderers
// that require both dimensions (the Word ImageRun).
export const scaleImage = (
    size: { height: number; width: number },
    maxWidth: number,
): { height: number; width: number } => {
    if (size.width <= 0 || size.height <= 0 || size.width <= maxWidth) {
        return { height: size.height, width: size.width };
    }

    const ratio = maxWidth / size.width;

    return { height: Math.round(size.height * ratio), width: maxWidth };
};

// arrayBufferToDataUrl feeds the renderers that take a data URL (the react-pdf
// image, the Word ImageRun data). Chunked so a large logo does not blow the
// call stack.
export const arrayBufferToDataUrl = (buffer: ArrayBuffer, mediaType: string): string => {
    const bytes = new Uint8Array(buffer);
    const chunk = 0x8000;
    let binary = '';

    for (let offset = 0; offset < bytes.length; offset += chunk) {
        binary += String.fromCharCode(...bytes.subarray(offset, offset + chunk));
    }

    return `data:${mediaType};base64,${btoa(binary)}`;
};
