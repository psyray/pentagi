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

const BODY_FONT = 'Calibri';
const MONO_FONT = 'Consolas';
const CODE_FILL = 'F4F5F7';
const HEADER_FILL = 'EDEFF2';
const BORDER_COLOR = 'D0D5DD';
const TEXT_COLOR = '1F2937';
const HEADING_COLOR = '1F3864';
const ORDERED_REFERENCE = 'report-ordered';

const thinBorder = { color: BORDER_COLOR, size: 4, style: BorderStyle.SINGLE } as const;

// depth 1 is the document title, 2 a section, 3+ sub-sections.
const HEADING_LEVELS = [
    HeadingLevel.TITLE,
    HeadingLevel.HEADING_1,
    HeadingLevel.HEADING_2,
    HeadingLevel.HEADING_3,
    HeadingLevel.HEADING_4,
    HeadingLevel.HEADING_5,
    HeadingLevel.HEADING_6,
];

const headingLevel = (depth: number): (typeof HEADING_LEVELS)[number] =>
    HEADING_LEVELS[Math.min(Math.max(depth, 1), HEADING_LEVELS.length) - 1] ?? HeadingLevel.HEADING_6;

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

const renderBlock = (block: Exclude<DocxBlock, { type: 'list' }>): Paragraph | Table => {
    switch (block.type) {
        case 'code':
            return codeParagraph(block.text);
        case 'heading':
            return new Paragraph({ children: [new TextRun(block.text)], heading: headingLevel(block.level) });
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

// Lists become one paragraph per item with a real bullet or number and a
// hanging indent, so wrapped lines align under the text, not the marker.
const renderDocxBlocks = (blocks: DocxBlock[]): (Paragraph | Table)[] =>
    blocks.flatMap((block): (Paragraph | Table)[] => {
        if (block.type === 'list') {
            return block.items.map(
                (item) =>
                    new Paragraph({
                        children: toRuns(item),
                        ...(block.ordered
                            ? { numbering: { level: 0, reference: ORDERED_REFERENCE } }
                            : { bullet: { level: 0 } }),
                    }),
            );
        }

        return [renderBlock(block)];
    });

export const markdownToDocxBlob = async (markdown: string): Promise<Blob> => {
    const doc = new Document({
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
                    paragraph: { spacing: { after: 120, before: 280 } },
                    run: { bold: true, color: HEADING_COLOR, size: 30 },
                },
                heading2: {
                    paragraph: { spacing: { after: 100, before: 240 } },
                    run: { bold: true, color: HEADING_COLOR, size: 26 },
                },
                heading3: {
                    paragraph: { spacing: { after: 80, before: 200 } },
                    run: { bold: true, color: '344054', size: 24 },
                },
                heading4: {
                    paragraph: { spacing: { after: 80, before: 180 } },
                    run: { bold: true, color: '344054', size: 22 },
                },
                heading5: { run: { bold: true, color: '344054', size: 22 } },
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
