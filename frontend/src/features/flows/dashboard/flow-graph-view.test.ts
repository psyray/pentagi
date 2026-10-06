import { describe, expect, it } from 'vitest';

import { buildLayout, columnForLabel, relatedNodes, toFlowEdges } from './flow-graph-view';

const node = (uuid: string, label: string, name = '') => ({
    createdAt: null,
    labels: [label],
    name,
    summary: '',
    uuid,
});

const edge = (uuid: string, source: string, target: string, type = 'HAS_PORT') => ({
    createdAt: null,
    fact: '',
    sourceUUID: source,
    targetUUID: target,
    type,
    uuid,
});

describe('flow-graph-view layout', () => {
    it('maps known labels to their lane and unknown labels past the mapped columns', () => {
        expect(columnForLabel('Host')).toBe(2);
        expect(columnForLabel('Service')).toBe(4);
        expect(columnForLabel('SomethingBrandNew')).toBeGreaterThan(9);
    });

    it('drops hidden labels from the graph (legend filter)', () => {
        const nodes = [node('h', 'Host'), node('p', 'Port'), node('a', 'Artifact')];

        const all = buildLayout(nodes, new Set());

        expect(all.rfNodes).toHaveLength(3);
        expect(all.positions.get('h')!.col).toBeLessThan(all.positions.get('p')!.col);

        const filtered = buildLayout(nodes, new Set(['Artifact']));

        expect(filtered.rfNodes.map((n) => n.id)).toEqual(['h', 'p']);
        expect(filtered.positions.has('a')).toBe(false);
    });

    it('reveals entities progressively with the replay cursor', () => {
        const nodes = [
            { ...node('a', 'Host'), createdAt: '2026-01-01T10:00:00Z' },
            { ...node('b', 'Port'), createdAt: '2026-01-01T10:05:00Z' },
            { ...node('c', 'Service'), createdAt: '2026-01-01T10:10:00Z' },
        ];

        const all = buildLayout(nodes, new Set(), null);

        expect(all.rfNodes).toHaveLength(3);

        const partial = buildLayout(nodes, new Set(), new Date('2026-01-01T10:06:00Z').getTime());

        expect(partial.rfNodes.map((n) => n.id)).toEqual(['a', 'b']);
    });
});

describe('flow-graph-view edge routing', () => {
    const positions = new Map([
        ['h', { col: 0, x: 0, y: 0 }],
        ['p', { col: 1, x: 340, y: 0 }],
        ['v', { col: 2, x: 680, y: 0 }],
    ]);

    const route = { busBottomY: 400, busTopY: 10, centerY: 200 };

    const routeOf = (e: undefined | { data?: unknown }) =>
        (e?.data as { route: { busY: number; gA: number; gB: number; kind: string } }).route;

    it('routes adjacent columns on the single gutter between them', () => {
        const edges = toFlowEdges([edge('e1', 'h', 'p')], {
            hoveredEdgeId: null,
            positions,
            related: null,
            route,
        });

        expect(edges).toHaveLength(1);
        expect(edges[0]!.type).toBe('routed');

        const r = routeOf(edges[0]);

        expect(r.kind).toBe('gutter');
        expect(r.gA).toBe(r.gB);
    });

    it('routes multi-column jumps on a bus while sharing the source-column gutter', () => {
        const edges = toFlowEdges([edge('e1', 'h', 'p'), edge('e2', 'h', 'v')], {
            hoveredEdgeId: null,
            positions,
            related: null,
            route,
        });

        const adjacent = routeOf(edges.find((e) => e.id === 'e1'));
        const jump = routeOf(edges.find((e) => e.id === 'e2'));

        expect(adjacent.kind).toBe('gutter');
        expect(jump.kind).toBe('bus');
        // Both leave the Host lane, so the trunk right after it is shared.
        expect(jump.gA).toBe(adjacent.gA);
        expect(jump.gB).not.toBe(jump.gA);
    });

    it('drops edges whose endpoint was filtered out', () => {
        const edges = toFlowEdges([edge('e1', 'h', 'missing')], {
            hoveredEdgeId: null,
            positions,
            related: null,
            route,
        });

        expect(edges).toHaveLength(0);
    });

    it('lights edges whose endpoints are in the related set and dims the rest', () => {
        const edges = toFlowEdges([edge('e1', 'h', 'p'), edge('e2', 'p', 'v')], {
            hoveredEdgeId: null,
            positions,
            related: new Set(['h', 'p']),
            route,
        });

        const lit = edges.find((e) => e.id === 'e1')!;
        const dimmed = edges.find((e) => e.id === 'e2')!;

        expect(lit.style?.opacity).toBe(1);
        expect(dimmed.style?.opacity).toBe(0.15);
    });
});

describe('flow-graph-view related set', () => {
    // h -> p -> s -> e -> v, plus a sibling branch p2 hanging off h.
    const graph = {
        edges: [
            edge('a', 'h', 'p'),
            edge('b', 'p', 's'),
            edge('c', 's', 'e'),
            edge('d', 'e', 'v'),
            edge('x', 'h', 'p2'),
        ],
    };

    it('lights ancestors and descendants of the anchor, not sibling branches', () => {
        const related = relatedNodes(graph, 'p');

        expect(related.has('h')).toBe(true); // ancestor
        expect(related.has('s')).toBe(true); // descendant
        expect(related.has('v')).toBe(true); // descendant, deeper
        expect(related.has('p2')).toBe(false); // sibling branch off h, not through p
    });

    it('handles cycles without looping', () => {
        const related = relatedNodes({ edges: [edge('a', 'h', 'p'), edge('b', 'p', 'h')] }, 'h');

        expect(related.has('p')).toBe(true);
        expect(related.size).toBe(2);
    });
});
