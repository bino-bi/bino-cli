import { LitElement, html, css, svg, nothing } from 'lit';

var NODE_W = 170;
var NODE_H = 38;
var H_GAP = 20;
var V_GAP = 56;
var PAD = 30;

var KIND_COLORS = {
  ReportArtefact:   { bg: '#dbeafe', stroke: '#3b82f6', text: '#1e40af' },
  DocumentArtefact: { bg: '#dbeafe', stroke: '#3b82f6', text: '#1e40af' },
  LayoutPage:       { bg: '#dcfce7', stroke: '#22c55e', text: '#166534' },
  LayoutCard:       { bg: '#ccfbf1', stroke: '#14b8a6', text: '#115e59' },
  Component:        { bg: '#f3e8ff', stroke: '#a855f7', text: '#6b21a8' },
  DataSet:          { bg: '#ffedd5', stroke: '#f97316', text: '#9a3412' },
  DataSource:       { bg: '#fee2e2', stroke: '#ef4444', text: '#991b1b' },
  MarkdownFile:     { bg: '#f3f4f6', stroke: '#9ca3af', text: '#374151' },
};

var SHORT_KIND = {
  ReportArtefact: 'Artefact',
  DocumentArtefact: 'DocArtefact',
  LayoutPage: 'Page',
  LayoutCard: 'Card',
  Component: 'Component',
  DataSet: 'DataSet',
  DataSource: 'Source',
  MarkdownFile: 'Markdown',
};

function colorFor(kind) {
  return KIND_COLORS[kind] || { bg: '#f3f4f6', stroke: '#9ca3af', text: '#374151' };
}

function shortKind(kind) {
  return SHORT_KIND[kind] || kind;
}

function truncName(name, max) {
  if (!max) max = 20;
  if (!name || name.length <= max) return name || '';
  return name.substring(0, max - 1) + '\u2026';
}

// Build tree from flat graph data, marking cycles and already-visited nodes as refs.
function buildTree(graphData) {
  if (!graphData || !graphData.rootId) return null;
  var nodes = graphData.nodes || {};
  var stack = {};
  var expanded = {};

  function walk(id) {
    if (!id) return null;
    var node = nodes[id];
    if (!node) return null;

    if (stack[id]) {
      return { id: id, kind: node.kind, name: node.name || id, children: [], cycle: true };
    }
    if (expanded[id]) {
      return { id: id, kind: node.kind, name: node.name || id, children: [], ref: true };
    }

    stack[id] = true;
    expanded[id] = true;

    var children = [];
    var deps = node.dependsOn || [];
    for (var i = 0; i < deps.length; i++) {
      var child = walk(deps[i]);
      if (child) children.push(child);
    }

    stack[id] = false;
    return { id: id, kind: node.kind, name: node.name || id, children: children };
  }

  return walk(graphData.rootId);
}

// Calculate subtree width bottom-up.
function calcWidth(t) {
  if (!t) return 0;
  if (t.children.length === 0) {
    t.w = NODE_W;
    return NODE_W;
  }
  var total = 0;
  for (var i = 0; i < t.children.length; i++) {
    total += calcWidth(t.children[i]);
  }
  total += (t.children.length - 1) * H_GAP;
  t.w = Math.max(total, NODE_W);
  return t.w;
}

// Assign positions top-down and collect positioned nodes + edges.
function layoutTree(tree) {
  if (!tree) return { nodes: [], edges: [], width: 0, height: 0 };

  calcWidth(tree);

  var positioned = [];
  var edges = [];
  var maxX = 0;
  var maxY = 0;

  function place(t, cx, y) {
    if (!t) return;
    var x = cx - NODE_W / 2;
    positioned.push({
      id: t.id, kind: t.kind, name: t.name,
      x: x, y: y, cx: cx,
      ref: t.ref || false, cycle: t.cycle || false,
    });
    if (cx + NODE_W / 2 > maxX) maxX = cx + NODE_W / 2;
    if (y + NODE_H > maxY) maxY = y + NODE_H;

    if (t.children.length === 0) return;
    var childY = y + NODE_H + V_GAP;
    var childrenW = 0;
    for (var i = 0; i < t.children.length; i++) childrenW += t.children[i].w;
    childrenW += (t.children.length - 1) * H_GAP;

    var sx = cx - childrenW / 2;
    for (var i = 0; i < t.children.length; i++) {
      var child = t.children[i];
      var ccx = sx + child.w / 2;
      edges.push({ x1: cx, y1: y + NODE_H, x2: ccx, y2: childY });
      place(child, ccx, childY);
      sx += child.w + H_GAP;
    }
  }

  place(tree, tree.w / 2 + PAD, PAD);
  return { nodes: positioned, edges: edges, width: maxX + PAD, height: maxY + PAD };
}

class BinoGraphModal extends LitElement {
  static properties = {
    _graphData: { state: true },
    _open: { state: true },
  };

  static styles = css`
    :host { font-family: var(--bino-font-sans); }
    .backdrop {
      position: fixed;
      inset: 0;
      background: rgba(0, 0, 0, 0.35);
      z-index: var(--bino-z-modal);
      display: flex;
      align-items: center;
      justify-content: center;
    }
    .modal {
      background: var(--bino-surface);
      border-radius: 12px;
      box-shadow: 0 20px 60px rgba(0, 0, 0, 0.2);
      width: 90vw;
      max-width: 1200px;
      max-height: 85vh;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .modal-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: var(--bino-space-md) var(--bino-space-lg);
      border-bottom: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .modal-header h2 {
      margin: 0;
      font-size: var(--bino-font-size-md);
      font-weight: 600;
      color: var(--bino-text);
    }
    .close-btn {
      background: none;
      border: none;
      font-size: 20px;
      cursor: pointer;
      color: var(--bino-text-secondary);
      padding: 0 4px;
      line-height: 1;
    }
    .close-btn:hover { color: var(--bino-text); }
    .graph-container {
      flex: 1;
      overflow: auto;
      min-height: 0;
      background: #fafbfc;
      padding: var(--bino-space-md);
    }
    svg { display: block; }
    svg text { font-family: var(--bino-font-sans); }
    .legend {
      display: flex;
      gap: var(--bino-space-md);
      flex-wrap: wrap;
      padding: var(--bino-space-sm) var(--bino-space-lg);
      border-top: 1px solid var(--bino-border);
      flex-shrink: 0;
    }
    .legend-item {
      display: flex;
      align-items: center;
      gap: var(--bino-space-xs);
      font-size: var(--bino-font-size-xs);
      color: var(--bino-text-secondary);
    }
    .legend-swatch {
      width: 12px;
      height: 12px;
      border-radius: 3px;
    }
    .empty {
      padding: var(--bino-space-xl);
      text-align: center;
      color: var(--bino-text-secondary);
      font-size: var(--bino-font-size-base);
    }
  `;

  constructor() {
    super();
    this._graphData = null;
    this._open = false;
    this._boundOnOpen = this._onOpen.bind(this);
    this._boundOnKeydown = this._onKeydown.bind(this);
  }

  connectedCallback() {
    super.connectedCallback();
    document.addEventListener('bino-open-graph', this._boundOnOpen);
    document.addEventListener('keydown', this._boundOnKeydown);
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener('bino-open-graph', this._boundOnOpen);
    document.removeEventListener('keydown', this._boundOnKeydown);
  }

  render() {
    if (!this._open) return nothing;

    var tree = buildTree(this._graphData);
    var layout = layoutTree(tree);

    return html`
      <div class='backdrop' @click=${this._close}>
        <div class='modal' @click=${this._stop}>
          <div class='modal-header'>
            <h2>Dependency Graph</h2>
            <button class='close-btn' title='Close' @click=${this._close}>&times;</button>
          </div>
          <div class='graph-container'>
            ${this._renderSVG(layout)}
          </div>
          ${this._renderLegend(layout)}
        </div>
      </div>
    `;
  }

  _renderSVG(layout) {
    if (!layout || layout.nodes.length === 0) {
      return html`<div class='empty'>No graph data available</div>`;
    }

    var w = layout.width;
    var h = layout.height;

    var edgePaths = layout.edges.map(function(e) {
      var midY = (e.y1 + e.y2) / 2;
      return svg`<path d=${'M' + e.x1 + ',' + e.y1 + ' C' + e.x1 + ',' + midY + ' ' + e.x2 + ',' + midY + ' ' + e.x2 + ',' + e.y2}
        fill='none' stroke='#cbd5e1' stroke-width='1.5'/>`;
    });

    var nodeGroups = layout.nodes.map(function(n) {
      var c = colorFor(n.kind);
      var op = (n.ref || n.cycle) ? '0.5' : '1';
      var kindLabel = shortKind(n.kind);
      var nameLabel = truncName(n.name);
      var refSuffix = n.cycle ? ' [cycle]' : (n.ref ? ' [ref]' : '');
      return svg`
        <g opacity=${op}>
          <rect x=${n.x} y=${n.y} width=${NODE_W} height=${NODE_H}
            rx='6' fill=${c.bg} stroke=${c.stroke} stroke-width='1.5'/>
          <text x=${n.cx} y=${n.y + 14} text-anchor='middle'
            font-size='10' font-weight='600' fill=${c.stroke}>${kindLabel}</text>
          <text x=${n.cx} y=${n.y + 28} text-anchor='middle'
            font-size='11' fill=${c.text}>${nameLabel}${refSuffix}</text>
        </g>
      `;
    });

    return html`
      <svg width=${w} height=${h} viewBox=${'0 0 ' + w + ' ' + h}>
        ${edgePaths}
        ${nodeGroups}
      </svg>
    `;
  }

  _renderLegend(layout) {
    if (!layout || layout.nodes.length === 0) return nothing;
    var seen = {};
    var kinds = [];
    layout.nodes.forEach(function(n) {
      if (!seen[n.kind]) {
        seen[n.kind] = true;
        kinds.push(n.kind);
      }
    });
    if (kinds.length === 0) return nothing;

    return html`
      <div class='legend'>
        ${kinds.map(function(k) {
          var c = colorFor(k);
          return html`
            <span class='legend-item'>
              <span class='legend-swatch' style=${'background:' + c.bg + ';border:1.5px solid ' + c.stroke}></span>
              ${k}
            </span>
          `;
        })}
      </div>
    `;
  }

  _onOpen(e) {
    this._graphData = (e.detail && e.detail.graph) || null;
    this._open = true;
  }

  _onKeydown(e) {
    if (this._open && e.key === 'Escape') {
      this._close();
    }
  }

  _close() {
    this._open = false;
  }

  _stop(e) {
    e.stopPropagation();
  }
}

customElements.define('bino-graph-modal', BinoGraphModal);
