import {
    BorderStyle,
    Document,
    HeadingLevel,
    Packer,
    Paragraph,
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

const HEADING_LEVELS = [
    HeadingLevel.HEADING_1,
    HeadingLevel.HEADING_2,
    HeadingLevel.HEADING_3,
    HeadingLevel.HEADING_4,
    HeadingLevel.HEADING_5,
    HeadingLevel.HEADING_6,
];

const toRun = (run: DocxInline): TextRun =>
    new TextRun({
        bold: run.bold,
        font: run.code ? 'Courier New' : undefined,
        italics: run.italics,
        text: run.text,
    });

const toRuns = (runs: DocxInline[]): TextRun[] => (runs.length > 0 ? runs.map(toRun) : [new TextRun('')]);

const toCell = (text: string): TableCell =>
    new TableCell({ children: [new Paragraph({ children: [new TextRun(text)] })] });

const renderBlock = (block: DocxBlock): Paragraph | Table => {
    switch (block.type) {
        case 'code':
            return new Paragraph({ children: [new TextRun({ font: 'Courier New', text: block.text })] });
        case 'heading':
            return new Paragraph({
                children: [new TextRun(block.text)],
                heading: HEADING_LEVELS[Math.min(block.level, HEADING_LEVELS.length) - 1] ?? HeadingLevel.HEADING_6,
            });
        case 'hr':
            return new Paragraph({
                border: { bottom: { color: '999999', size: 6, style: BorderStyle.SINGLE } },
                text: '',
            });
        case 'list':
            return new Paragraph({
                children: toRuns(block.items.flat()),
            });
        case 'quote':
            return new Paragraph({ children: toRuns(block.runs), style: 'IntenseQuote' });
        case 'table':
            return new Table({
                rows: [
                    new TableRow({ children: block.header.map(toCell) }),
                    ...block.rows.map((row) => new TableRow({ children: row.map(toCell) })),
                ],
                width: { size: 100, type: WidthType.PERCENTAGE },
            });
        default:
            return new Paragraph({ children: toRuns(block.runs) });
    }
};

// renderDocxBlocks flattens lists into one row per item so a list keeps its
// bullets/numbers without a document-level numbering definition.
const renderDocxBlocks = (blocks: DocxBlock[]): (Paragraph | Table)[] =>
    blocks.flatMap((block): (Paragraph | Table)[] => {
        if (block.type === 'list') {
            return block.items.map(
                (item, index) =>
                    new Paragraph({
                        children: [new TextRun(block.ordered ? `${index + 1}. ` : '• '), ...toRuns(item)],
                    }),
            );
        }

        return [renderBlock(block)];
    });

export const markdownToDocxBlob = async (markdown: string): Promise<Blob> => {
    const doc = new Document({
        sections: [{ children: renderDocxBlocks(parseReportMarkdown(markdown)) }],
    });

    return Packer.toBlob(doc);
};
