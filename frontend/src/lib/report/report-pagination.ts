// Shared pagination rule for the assembled report document (DOCX and PDF
// exports). The server writes every section as a level-2 heading, so the
// outline is carried by the number, not the depth: "7. Title" is a chapter,
// "7.1. Title" is a finding (or recommendation), and an unnumbered level-2
// section is front matter. Level 1 is the document title; level 3+ headings
// belong to a section's body and never start a page.

export type HeadingKind = 'chapter' | 'content' | 'front' | 'subsection' | 'title';

const DOTTED_NUMBER = /^\d+\.\d/;
const SINGLE_NUMBER = /^\d+\./;

export const headingKind = (level: number, text: string): HeadingKind => {
    if (level === 1) {
        return 'title';
    }

    if (level >= 3) {
        return 'content';
    }

    if (DOTTED_NUMBER.test(text)) {
        return 'subsection';
    }

    return SINGLE_NUMBER.test(text) ? 'chapter' : 'front';
};

export interface HeadingDecision {
    /** A page break must precede this heading. */
    breakBefore: boolean;
    /** The table of contents belongs right before this heading (after the cover). */
    insertTocBefore: boolean;
    kind: HeadingKind;
}

/**
 * Stateful walker over a document's blocks, in order. Feed every heading to
 * `heading` and every non-heading block to `content`; each call reports whether
 * the emitted element starts a fresh page.
 *
 * Every chapter opens a page; every subsection opens one too, except the first
 * of its chapter, which already sits at the top of the page the chapter opened.
 */
export class ReportPaginator {
    private level2Seen = 0;
    private pendingChild = false;

    content(): void {
        this.pendingChild = false;
    }

    heading(level: number, text: string): HeadingDecision {
        const kind = headingKind(level, text);

        if (kind === 'title') {
            return { breakBefore: false, insertTocBefore: false, kind };
        }

        if (kind === 'content') {
            this.pendingChild = false;

            return { breakBefore: false, insertTocBefore: false, kind };
        }

        if (kind === 'subsection') {
            const breakBefore = !this.pendingChild;

            this.pendingChild = false;

            return { breakBefore, insertTocBefore: false, kind };
        }

        this.level2Seen += 1;

        const insertTocBefore = this.level2Seen === 2;
        const breakBefore = this.level2Seen > 1;

        this.pendingChild = true;

        return { breakBefore, insertTocBefore, kind };
    }
}
