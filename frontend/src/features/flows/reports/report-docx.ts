import {
    AlignmentType,
    BorderStyle,
    Document,
    HeadingLevel,
    LevelFormat,
    Packer,
    Paragraph,
    ShadingType,
    Table,
    TableCell,
    TableOfContents,
    TableRow,
    TextRun,
    WidthType,
} from 'docx';
import { marked } from 'marked';

export type DocxBlock =
    | { header: string[]; rows: string[][]; type: 'table' }
    | { items: DocxInline[][]; ordered: boolean; type: 'list' }
    | { level: number; text: string; type: 'heading' }
    | { runs: DocxInline[]; type: 'paragraph' }
    | { runs: DocxInline[]; type: 'quote' }
    | { text: string; type: 'code' }
    | { type: 'hr' };

// Lightweight, testable intermediate between the markdown we assemble and the
// docx elements. Keeping the token walk pure lets the mapping be unit-tested
// without a ZIP/OOXML round-trip.
export interface DocxInline {
    bold?: boolean;
    code?: boolean;
    italics?: boolean;
    link?: string;
    text: string;
}

export type DocxLeafBlock = Extract<DocxBlock, { type: 'code' | 'hr' | 'paragraph' | 'quote' | 'table' }>;

const inlineRuns = (text: string): DocxInline[] => {
    const [first] = marked.lexer(text, { breaks: false });

    if (!first || first.type !== 'paragraph' || !('tokens' in first) || !first.tokens?.length) {
        return text ? [{ text }] : [];
    }

    const runs: DocxInline[] = [];

    for (const token of first.tokens as Record<string, unknown>[]) {
        switch (token.type) {
            case 'codespan':
                runs.push({ code: true, text: String(token.text ?? '') });
                break;
            case 'em':
                runs.push({ italics: true, text: String(token.text ?? '') });
                break;
            case 'link':
                runs.push({ link: String(token.href ?? ''), text: String(token.text ?? '') });
                break;
            case 'strong':
                runs.push({ bold: true, text: String(token.text ?? '') });
                break;
            case 'text':
                runs.push({ text: String(token.text ?? '') });
                break;
            default:
                if (typeof token.text === 'string') {
                    runs.push({ text: token.text });
                }
        }
    }

    return runs;
};

// parseReportMarkdown maps the assembled document markdown to blocks. Headings,
// paragraphs, lists, code, quotes, rules and (GFM) tables are covered — the
// shapes a report actually emits.
export const parseReportMarkdown = (markdown: string): DocxBlock[] => {
    const blocks: DocxBlock[] = [];

    for (const token of marked.lexer(markdown) as Record<string, unknown>[]) {
        switch (token.type) {
            case 'blockquote':
                blocks.push({ runs: inlineRuns(String(token.text ?? '')), type: 'quote' });
                break;
            case 'code':
                blocks.push({ text: String(token.text ?? ''), type: 'code' });
                break;
            case 'heading':
                blocks.push({ level: Number(token.depth ?? 1), text: String(token.text ?? ''), type: 'heading' });
                break;
            case 'hr':
                blocks.push({ type: 'hr' });
                break;
            case 'list':
                blocks.push({
                    items: ((token.items as Record<string, unknown>[]) ?? []).map((item) =>
                        inlineRuns(String(item.text ?? '')),
                    ),
                    ordered: Boolean(token.ordered),
                    type: 'list',
                });
                break;
            case 'paragraph':
                blocks.push({ runs: inlineRuns(String(token.text ?? '')), type: 'paragraph' });
                break;
            case 'space':
                break;
            case 'table':
                blocks.push({
                    header: ((token.header as Record<string, unknown>[]) ?? []).map((cell) => String(cell.text ?? '')),
                    rows: ((token.rows as Record<string, unknown>[][]) ?? []).map((row) =>
                        row.map((cell) => String(cell.text ?? '')),
                    ),
                    type: 'table',
                });
                break;
            default:
                if (typeof token.text === 'string') {
                    blocks.push({ runs: inlineRuns(token.text), type: 'paragraph' });
                }
        }
    }

    return blocks;
};

// Shape of a table-of-contents entry (docx's ToCEntry is not exported).
export interface ReportTocEntry {
    level: number;
    title: string;
}

// The server writes every section as a level-2 heading; the outline is carried
// by the number, not the depth: "7. Title" is a chapter, "7.1. Title" is a
// finding (or recommendation), and an unnumbered level-2 section is front
// matter. Level 3+ headings belong to a section's body, never the outline.
type HeadingKind = 'chapter' | 'content' | 'front' | 'subsection' | 'title';

const DOTTED_NUMBER = /^\d+\.\d/;
const SINGLE_NUMBER = /^\d+\./;

const headingKind = (block: Extract<DocxBlock, { type: 'heading' }>): HeadingKind => {
    if (block.level === 1) {
        return 'title';
    }

    if (block.level >= 3) {
        return 'content';
    }

    if (DOTTED_NUMBER.test(block.text)) {
        return 'subsection';
    }

    return SINGLE_NUMBER.test(block.text) ? 'chapter' : 'front';
};

// reportTocEntries lists the document outline (chapters at level 1, findings
// and recommendations at level 2), skipping the document title and the cover
// section, which is the document's own cover page.
export const reportTocEntries = (blocks: readonly DocxBlock[]): ReportTocEntry[] => {
    const entries: ReportTocEntry[] = [];
    let level2Seen = 0;

    for (const block of blocks) {
        if (block.type !== 'heading' || block.level !== 2) {
            continue;
        }

        level2Seen += 1;

        if (level2Seen === 1) {
            continue;
        }

        entries.push({ level: DOTTED_NUMBER.test(block.text) ? 2 : 1, title: block.text });
    }

    return entries;
};

const BODY_FONT = 'Calibri';
const MONO_FONT = 'Consolas';
const CODE_FILL = 'F4F5F7';
const HEADER_FILL = 'EDEFF2';
const BORDER_COLOR = 'D0D5DD';
const TEXT_COLOR = '1F2937';
const HEADING_COLOR = '1F3864';
const SUBHEADING_COLOR = '344054';
const ORDERED_REFERENCE = 'report-ordered';

const thinBorder = { color: BORDER_COLOR, size: 4, style: BorderStyle.SINGLE } as const;

const toRun = (run: DocxInline): TextRun =>
    new TextRun({
        bold: run.bold,
        font: run.code ? MONO_FONT : undefined,
        italics: run.italics,
        size: run.code ? 20 : undefined,
        text: run.text,
    });

const toRuns = (runs: DocxInline[]): TextRun[] => (runs.length > 0 ? runs.map(toRun) : [new TextRun('')]);

// A monospaced, lightly shaded block; docx has no line-break-in-run, so each
// line becomes a run with a leading break.
const codeParagraph = (text: string): Paragraph => {
    const lines = text.replace(/\n$/, '').split('\n');

    return new Paragraph({
        children: lines.flatMap((line, index) =>
            index === 0
                ? [new TextRun({ font: MONO_FONT, size: 18, text: line })]
                : [new TextRun({ break: 1, font: MONO_FONT, size: 18, text: line })],
        ),
        shading: { fill: CODE_FILL, type: ShadingType.CLEAR },
        spacing: { after: 160, before: 80 },
    });
};

const headerCell = (text: string): TableCell =>
    new TableCell({
        children: [new Paragraph({ children: [new TextRun({ bold: true, text })], spacing: { after: 0 } })],
        shading: { fill: HEADER_FILL, type: ShadingType.CLEAR },
    });

const bodyCell = (text: string): TableCell =>
    new TableCell({ children: [new Paragraph({ children: [new TextRun(text)], spacing: { after: 0 } })] });

// A heading inside a section body stays a bold paragraph rather than a real
// heading style, so it never lands in the table of contents.
const contentHeading = (text: string): Paragraph =>
    new Paragraph({
        children: [new TextRun({ bold: true, color: SUBHEADING_COLOR, size: 24, text })],
        spacing: { after: 80, before: 200 },
    });

const renderLeaf = (block: DocxLeafBlock): Paragraph | Table => {
    switch (block.type) {
        case 'code':
            return codeParagraph(block.text);
        case 'hr':
            return new Paragraph({
                border: { bottom: { color: BORDER_COLOR, size: 6, style: BorderStyle.SINGLE } },
                spacing: { after: 200, before: 200 },
                text: '',
            });
        case 'quote':
            return new Paragraph({ children: toRuns(block.runs), style: 'IntenseQuote' });
        case 'table':
            return new Table({
                borders: {
                    bottom: thinBorder,
                    insideHorizontal: thinBorder,
                    insideVertical: thinBorder,
                    left: thinBorder,
                    right: thinBorder,
                    top: thinBorder,
                },
                rows: [
                    new TableRow({ children: block.header.map(headerCell), tableHeader: true }),
                    ...block.rows.map((row) => new TableRow({ children: row.map(bodyCell) })),
                ],
                width: { size: 100, type: WidthType.PERCENTAGE },
            });
        default:
            return new Paragraph({ children: toRuns(block.runs) });
    }
};

const listParagraph = (item: DocxInline[], ordered: boolean): Paragraph =>
    new Paragraph({
        children: toRuns(item),
        ...(ordered ? { numbering: { level: 0, reference: ORDERED_REFERENCE } } : { bullet: { level: 0 } }),
    });

const tocBlocks = (entries: readonly ReportTocEntry[]): (Paragraph | TableOfContents)[] => [
    new Paragraph({
        children: [new TextRun({ bold: true, color: HEADING_COLOR, size: 30, text: 'Table of Contents' })],
        pageBreakBefore: true,
        spacing: { after: 200, before: 0 },
    }),
    new TableOfContents('', {
        beginDirty: true,
        cachedEntries: entries,
        headingStyleRange: '1-2',
        hyperlink: true,
    }),
];

export type DocxChild = Paragraph | Table | TableOfContents;

// Chapters and findings each start on a fresh page: a page break precedes every
// level-2 heading but the cover, and every subsection but the first of its
// chapter (already at the top of the page its chapter opened).
export const renderDocxBlocks = (blocks: readonly DocxBlock[]): DocxChild[] => {
    const out: DocxChild[] = [];
    const entries = reportTocEntries(blocks);
    let level2Seen = 0;
    let pendingChild = false;

    for (const block of blocks) {
        if (block.type === 'heading') {
            const kind = headingKind(block);

            if (kind === 'title') {
                out.push(new Paragraph({ children: [new TextRun(block.text)], heading: HeadingLevel.TITLE }));
                continue;
            }

            if (kind === 'content') {
                pendingChild = false;
                out.push(contentHeading(block.text));
                continue;
            }

            if (kind === 'subsection') {
                out.push(
                    new Paragraph({
                        children: [new TextRun(block.text)],
                        heading: HeadingLevel.HEADING_2,
                        pageBreakBefore: !pendingChild,
                    }),
                );
                pendingChild = false;
                continue;
            }

            level2Seen += 1;

            if (level2Seen === 2) {
                out.push(...tocBlocks(entries));
            }

            out.push(
                new Paragraph({
                    children: [new TextRun(block.text)],
                    heading: HeadingLevel.HEADING_1,
                    pageBreakBefore: level2Seen > 1,
                }),
            );
            pendingChild = true;
            continue;
        } else if (block.type === 'list') {
            pendingChild = false;
            out.push(...block.items.map((item) => listParagraph(item, block.ordered)));
            continue;
        }

        pendingChild = false;
        out.push(renderLeaf(block));
    }

    return out;
};

export const markdownToDocxBlob = async (markdown: string): Promise<Blob> => {
    const doc = new Document({
        features: { updateFields: true },
        numbering: {
            config: [
                {
                    levels: [
                        {
                            alignment: AlignmentType.START,
                            format: LevelFormat.DECIMAL,
                            level: 0,
                            style: { paragraph: { indent: { hanging: 360, left: 720 } } },
                            text: '%1.',
                        },
                    ],
                    reference: ORDERED_REFERENCE,
                },
            ],
        },
        sections: [
            {
                children: renderDocxBlocks(parseReportMarkdown(markdown)),
                properties: { page: { margin: { bottom: 1134, left: 1134, right: 1134, top: 1134 } } },
            },
        ],
        styles: {
            default: {
                document: {
                    paragraph: { alignment: AlignmentType.LEFT, spacing: { after: 120, line: 276 } },
                    run: { color: TEXT_COLOR, font: BODY_FONT, size: 22 },
                },
                heading1: {
                    paragraph: { spacing: { after: 120, before: 0 } },
                    run: { bold: true, color: HEADING_COLOR, size: 30 },
                },
                heading2: {
                    paragraph: { spacing: { after: 100, before: 160 } },
                    run: { bold: true, color: HEADING_COLOR, size: 26 },
                },
                listParagraph: { paragraph: { spacing: { after: 60 } } },
                title: {
                    paragraph: { alignment: AlignmentType.LEFT, spacing: { after: 240 } },
                    run: { bold: true, color: HEADING_COLOR, size: 44 },
                },
            },
        },
    });

    return Packer.toBlob(doc);
};
