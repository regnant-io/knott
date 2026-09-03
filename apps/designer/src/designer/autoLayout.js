/**
 * Tidy a workflow graph into left-to-right layers.
 *
 * A workflow is a DAG most of the time, and reads best as columns: the trigger
 * on the left, each step to the right of whatever feeds it. Cycles do occur —
 * a revision loop sends work back for another pass — so the layering assigns a
 * depth by longest path from the roots and simply ignores the back edges rather
 * than refusing to lay the graph out.
 *
 * The ordering pass is the barycentre heuristic: repeatedly place each node at
 * the average position of its neighbours in the previous layer. A few sweeps
 * remove most edge crossings, which is what actually makes a graph readable.
 */

const COLUMN_GAP = 260;
const ROW_GAP = 130;
const ORIGIN = { x: 80, y: 80 };
const SWEEPS = 6;

/**
 * Returns new positions keyed by node id. Nodes marked as annotations (notes)
 * keep their positions — they are placed deliberately.
 */
export function autoLayout(nodes, edges, { isAnnotation = () => false } = {}) {
  const laidOut = nodes.filter(n => !isAnnotation(n));
  if (laidOut.length === 0) return {};

  const ids = new Set(laidOut.map(n => n.id));
  const outgoing = new Map(laidOut.map(n => [n.id, []]));
  const incoming = new Map(laidOut.map(n => [n.id, []]));
  for (const e of edges) {
    if (!ids.has(e.source) || !ids.has(e.target) || e.source === e.target) continue;
    outgoing.get(e.source).push(e.target);
    incoming.get(e.target).push(e.source);
  }

  // Roots: anything nothing points at. A graph that is all cycles has none, so
  // fall back to the first node to keep the layout deterministic.
  let roots = laidOut.filter(n => incoming.get(n.id).length === 0).map(n => n.id);
  if (roots.length === 0) roots = [laidOut[0].id];

  // Depth by longest path from a root, following forward edges only. The visited
  // set per traversal is what keeps a cycle from recursing forever.
  const depth = new Map(laidOut.map(n => [n.id, 0]));
  const queue = roots.map(id => ({ id, d: 0, seen: new Set([id]) }));
  while (queue.length) {
    const { id, d, seen } = queue.shift();
    if (d > depth.get(id)) depth.set(id, d);
    for (const next of outgoing.get(id)) {
      if (seen.has(next)) continue; // back edge: a loop, not a deeper layer
      if (d + 1 > depth.get(next)) {
        depth.set(next, d + 1);
        queue.push({ id: next, d: d + 1, seen: new Set([...seen, next]) });
      }
    }
  }

  // Bucket into layers, seeded in the graph's own order for stability.
  const layers = [];
  for (const n of laidOut) {
    const d = depth.get(n.id);
    (layers[d] || (layers[d] = [])).push(n.id);
  }
  for (let i = 0; i < layers.length; i++) if (!layers[i]) layers[i] = [];

  // Barycentre sweeps: order each layer by the mean position of its neighbours
  // in the adjacent one, alternating direction.
  const rank = new Map();
  layers.forEach(layer => layer.forEach((id, i) => rank.set(id, i)));

  const order = (layer, neighbours) => {
    const scored = layer.map(id => {
      const ns = neighbours(id).filter(n => rank.has(n));
      const bary = ns.length
        ? ns.reduce((sum, n) => sum + rank.get(n), 0) / ns.length
        : rank.get(id);
      return { id, bary };
    });
    scored.sort((a, b) => a.bary - b.bary);
    scored.forEach((s, i) => rank.set(s.id, i));
    return scored.map(s => s.id);
  };

  for (let sweep = 0; sweep < SWEEPS; sweep++) {
    if (sweep % 2 === 0) {
      for (let i = 1; i < layers.length; i++) {
        layers[i] = order(layers[i], id => incoming.get(id));
      }
    } else {
      for (let i = layers.length - 2; i >= 0; i--) {
        layers[i] = order(layers[i], id => outgoing.get(id));
      }
    }
  }

  // Place. Each layer is centred vertically against the tallest one, so the
  // graph reads along a spine rather than hanging off the top edge.
  const tallest = Math.max(...layers.map(l => l.length), 1);
  const positions = {};
  layers.forEach((layer, column) => {
    const offset = ((tallest - layer.length) * ROW_GAP) / 2;
    layer.forEach((id, row) => {
      positions[id] = {
        x: ORIGIN.x + column * COLUMN_GAP,
        y: ORIGIN.y + offset + row * ROW_GAP,
      };
    });
  });
  return positions;
}

/**
 * Where to put a node appended after `source`: one column to the right, and
 * nudged down past anything already occupying that spot so a new branch does
 * not land on top of an existing one.
 */
export function positionAfter(source, nodes) {
  const base = {
    x: (source?.position?.x ?? ORIGIN.x) + COLUMN_GAP,
    y: source?.position?.y ?? ORIGIN.y,
  };
  const collides = p => nodes.some(n =>
    Math.abs(n.position.x - p.x) < COLUMN_GAP * 0.6 &&
    Math.abs(n.position.y - p.y) < ROW_GAP * 0.7);
  while (collides(base)) base.y += ROW_GAP;
  return base;
}
