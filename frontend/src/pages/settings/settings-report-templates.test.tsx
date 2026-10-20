import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const auth = vi.hoisted(() => ({ privileges: ['reports.edit', 'reports.read'] as string[] }));

const queryResult = vi.hoisted(() => ({
    current: { data: undefined, error: undefined, loading: false, refetch: () => {} } as Record<string, unknown>,
}));

const { mutate } = vi.hoisted(() => ({ mutate: vi.fn(async () => ({ data: {} })) }));

vi.mock('@apollo/client/react', () => ({
    useMutation: () => [mutate, { loading: false }],
    useQuery: () => queryResult.current,
}));

vi.mock('@/providers/user-provider', () => ({
    useUser: () => ({ authInfo: { privileges: auth.privileges } }),
}));

// The YAML widget is a third-party editor (CodeMirror) that does not respond to
// synthetic typing in jsdom; the page test drives it through a plain textarea.
vi.mock('@uiw/react-codemirror', () => ({
    default: ({
        'aria-label': ariaLabel,
        onChange,
        value,
    }: {
        'aria-label'?: string;
        onChange?: (value: string) => void;
        value?: string;
    }) => (
        <textarea
            aria-label={ariaLabel}
            onChange={(event) => onChange?.(event.target.value)}
            value={value}
        />
    ),
    EditorView: { lineWrapping: [] },
}));

vi.mock('@/components/layouts/app/app-header', () => {
    const Pass = ({ children }: { children?: React.ReactNode }) => <div>{children}</div>;
    const Action = ({ label, onClick }: { label?: React.ReactNode; onClick?: () => void }) => (
        <button
            onClick={onClick}
            type="button"
        >
            {label}
        </button>
    );

    return {
        AppHeader: Pass,
        AppHeaderAction: Action,
        AppHeaderActions: Pass,
        AppHeaderContent: Pass,
        AppHeaderTitle: Pass,
    };
});

import { ReportTemplateSource } from '@/graphql/types';

import SettingsReportTemplates from './settings-report-templates';

const branding = {
    footer: null,
    hasLogo: false,
    header: null,
    logoMediaType: null,
    logoName: null,
    subtitle: null,
    title: null,
};

const bundledTemplate = {
    branding,
    createdAt: null,
    enabled: true,
    id: '0',
    key: 'pentagi-default-pentest-report',
    label: 'PentAGI Penetration Testing Report',
    source: ReportTemplateSource.Bundled,
    updatedAt: null,
    userId: null,
    version: 1,
    yaml: '',
};

const userTemplate = {
    ...bundledTemplate,
    createdAt: '2026-01-01T00:00:00Z',
    id: '7',
    key: 'my-template',
    label: 'My Template',
    source: ReportTemplateSource.User,
    updatedAt: '2026-01-02T00:00:00Z',
    userId: '42',
    version: 2,
    yaml: 'key: my-template\nlabel: My Template\nsections: []',
};

const globalTemplate = {
    ...bundledTemplate,
    createdAt: '2026-01-01T00:00:00Z',
    id: '9',
    key: 'team-template',
    label: 'Team Template',
    source: ReportTemplateSource.Global,
    updatedAt: '2026-01-02T00:00:00Z',
    userId: null,
};

const makeData = (templates: unknown[]) => ({ reportTemplates: templates });

const renderPage = () =>
    render(
        <MemoryRouter>
            <SettingsReportTemplates />
        </MemoryRouter>,
    );

const openFirstRowMenu = async (user: ReturnType<typeof userEvent.setup>) => {
    const triggers = screen.getAllByRole('button', { name: /open menu/i });

    await user.click(triggers[0] as HTMLElement);
};

describe('Settings > Report templates', () => {
    beforeEach(() => {
        mutate.mockClear();
        auth.privileges = ['reports.edit', 'reports.read'];
        queryResult.current = { data: undefined, error: undefined, loading: false, refetch: () => {} };
    });

    it('renders the merged list with a source badge per row', () => {
        queryResult.current = {
            data: makeData([userTemplate, bundledTemplate]),
            error: undefined,
            loading: false,
            refetch: () => {},
        };

        renderPage();

        expect(screen.getByText('My Template')).toBeInTheDocument();
        expect(screen.getByText('my-template')).toBeInTheDocument();
        expect(screen.getByText('PentAGI Penetration Testing Report')).toBeInTheDocument();
        expect(screen.getByText('Mine')).toBeInTheDocument();
        expect(screen.getByText('Bundled')).toBeInTheDocument();
    });

    it('creates a template with the YAML typed in the editor', async () => {
        queryResult.current = {
            data: makeData([bundledTemplate]),
            error: undefined,
            loading: false,
            refetch: () => {},
        };
        const user = userEvent.setup();

        renderPage();
        await user.click(screen.getByRole('button', { name: /new template/i }));

        const editor = screen.getByLabelText(/report template yaml/i);

        await user.type(editor, 'key: new-tpl');

        await user.click(screen.getByRole('button', { name: /create template/i }));

        expect(mutate).toHaveBeenCalledWith(
            expect.objectContaining({
                variables: { input: { enabled: true, global: false, yaml: 'key: new-tpl' } },
            }),
        );
    });

    it('offers no delete action on a bundled template', async () => {
        queryResult.current = {
            data: makeData([bundledTemplate]),
            error: undefined,
            loading: false,
            refetch: () => {},
        };
        const user = userEvent.setup();

        renderPage();
        await openFirstRowMenu(user);

        expect(screen.getByRole('menuitem', { name: /edit yaml/i })).toBeInTheDocument();
        expect(screen.queryByRole('menuitem', { name: /delete/i })).not.toBeInTheDocument();
        expect(screen.queryByRole('menuitem', { name: /branding/i })).not.toBeInTheDocument();
    });

    it('disables a template through its toggle mutation', async () => {
        queryResult.current = {
            data: makeData([userTemplate]),
            error: undefined,
            loading: false,
            refetch: () => {},
        };
        const user = userEvent.setup();

        renderPage();
        await openFirstRowMenu(user);
        await user.click(screen.getByRole('menuitem', { name: /disable/i }));

        expect(mutate).toHaveBeenCalledWith(
            expect.objectContaining({ variables: { enabled: false, templateId: '7' } }),
        );
    });

    it('offers no action on a global template when the caller is not an admin', () => {
        auth.privileges = ['reports.edit', 'reports.read'];
        queryResult.current = {
            data: makeData([globalTemplate]),
            error: undefined,
            loading: false,
            refetch: () => {},
        };

        renderPage();

        expect(screen.queryAllByRole('button', { name: /open menu/i })).toHaveLength(0);
    });
});
