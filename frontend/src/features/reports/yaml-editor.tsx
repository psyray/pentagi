import { yaml } from '@codemirror/lang-yaml';
import CodeMirror, { EditorView } from '@uiw/react-codemirror';
import { useMemo } from 'react';

import { useTheme } from '@/hooks/use-theme';
import { type Theme } from '@/providers/theme-provider';

interface YamlEditorProps {
    'aria-label': string;
    /** Rejection-free handler: the caller owns validation and error display. */
    onChange: (value: string) => void;
    /** Built-in templates open read-only; no editing affordances then. */
    readOnly?: boolean;
    value: string;
}

// YamlEditor edits a report-template outline: CodeMirror 6 with YAML syntax
// highlighting, line numbers, folding and bracket matching, following the app
// theme. It is the source-of-truth document for a template (key/label/version
// included), so the backend re-validates it on save. Long instruction lines wrap
// rather than widen the editor past its dialog.
export function YamlEditor({ 'aria-label': ariaLabel, onChange, readOnly = false, value }: YamlEditorProps) {
    const { theme } = useTheme();
    const isDark = useMemo(() => isDarkTheme(theme), [theme]);

    return (
        <div className="min-w-0 overflow-hidden rounded-md border">
            <CodeMirror
                aria-label={ariaLabel}
                basicSetup={{
                    bracketMatching: true,
                    foldGutter: true,
                    highlightActiveLine: !readOnly,
                    highlightActiveLineGutter: !readOnly,
                    lineNumbers: true,
                }}
                editable={!readOnly}
                extensions={[yaml(), EditorView.lineWrapping]}
                height="clamp(16rem, 55vh, 36rem)"
                onChange={onChange}
                readOnly={readOnly}
                theme={isDark ? 'dark' : 'light'}
                value={value}
                width="100%"
            />
        </div>
    );
}

function isDarkTheme(theme: Theme): boolean {
    if (theme === 'dark') {
        return true;
    }

    if (theme === 'light') {
        return false;
    }

    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
        return false;
    }

    return window.matchMedia('(prefers-color-scheme: dark)').matches;
}
