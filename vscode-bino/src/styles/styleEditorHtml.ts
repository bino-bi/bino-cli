import { editorPage } from './editorPage';

/** The page of the ComponentStyle editor. */
export function getStyleEditorHtml(nonce: string): string {
    return editorPage(nonce, 'ComponentStyle Editor', CSS, SCRIPT);
}

const CSS = `
    .tabs { display: flex; flex-wrap: wrap; gap: 2px; border-bottom: 1px solid var(--vscode-panel-border); margin: 8px 0; }
    .tab { background: transparent; color: var(--vscode-foreground); border-bottom: 2px solid transparent; border-radius: 0; padding: 4px 8px; }
    .tab.active { border-bottom-color: var(--vscode-focusBorder); }
    .tab .dot { color: var(--vscode-textLink-foreground); }
    .row { display: grid; grid-template-columns: 200px 1fr; gap: 8px; align-items: center; padding: 3px 0; }
    .row > label { overflow: hidden; text-overflow: ellipsis; }
    .row > label.set { font-weight: bold; }
    fieldset { border: 1px solid var(--vscode-panel-border); margin: 8px 0; padding: 4px 10px 8px; }
    legend { padding: 0 4px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 2px 14px; }
    .grid .row { grid-template-columns: 150px 1fr; }
    table.slots { border-collapse: collapse; }
    table.slots td, table.slots th { padding: 2px 4px; text-align: left; font-weight: normal; }
    table.slots th { color: var(--vscode-descriptionForeground); font-size: 0.85em; }
    table.slots input[type=number] { width: 56px; }
    table.slots input.color-text { width: 72px; }
    .list-item { display: flex; align-items: center; gap: 4px; margin: 2px 0; }
    .chip { display: inline-block; width: 14px; height: 14px; border: 1px solid var(--vscode-panel-border); vertical-align: middle; margin-right: 2px; }
`;

const SCRIPT = `
    let state;
    let tab = saved.tab;

    function edit(path, value) { post({ type: 'edit', path: path, value: value }); }
    function clear(path) { post({ type: 'remove', path: path }); }
    function sourceText(node) {
        if (node.source === '_default') return 'from _default';
        if (node.source === '_system') return 'engine default';
        return '';
    }
    function key(path) { return path.join('/'); }

    function leaf(node) {
        const k = key(node.path);
        const set = node.own !== undefined;
        let control;
        if (node.input === 'enum' || node.input === 'bool') {
            const options = node.input === 'bool' ? ['true', 'false'] : node.options;
            const sel = h('select', { 'data-key': k }, [h('option', { value: '', text: node.inherited === undefined ? '(not set)' : '(inherit: ' + show(node.inherited) + ')' })]
                .concat(options.map(o => h('option', { value: o, selected: show(node.own) === o, text: o }))));
            sel.addEventListener('change', () => {
                if (sel.value === '') clear(node.path);
                else edit(node.path, node.input === 'bool' ? sel.value === 'true' : sel.value);
            });
            control = sel;
        } else if (node.input === 'color') {
            control = colorInput(k, node.own, node.inherited, v => v === '' ? clear(node.path) : edit(node.path, v));
        } else {
            const input = h('input', { 'data-key': k, type: node.input === 'number' ? 'number' : 'text', step: 'any', value: show(node.own), placeholder: show(node.inherited) });
            input.addEventListener('change', () => {
                if (node.input === 'number') {
                    const n = numberOf(input);
                    if (n === undefined) clear(node.path);
                    else if (!isNaN(n)) edit(node.path, n);
                    return;
                }
                const raw = input.value.trim();
                if (raw === '') clear(node.path); else edit(node.path, raw);
            });
            control = input;
        }
        return h('div', { class: 'row', title: node.description || '' }, [
            h('label', { class: set ? 'set' : '', text: node.label }),
            h('span', {}, [control, set ? null : h('span', { class: 'src', text: sourceText(node) })]),
        ]);
    }

    function group(node) {
        return h('fieldset', { title: node.description || '' }, [h('legend', { text: node.label }), h('div', { class: 'grid' }, node.children.map(render))]);
    }

    function slots(node) {
        const head = h('tr', {}, [h('th', { text: '' }), h('th', { text: 'look' })].concat(node.keys.map(k => h('th', { text: k }))));
        const rows = node.slots.map(slot => {
            const own = node.own[slot] || {};
            const inh = node.inherited[slot] || {};
            const value = k => own[k];
            const ph = k => inh[k];
            const cells = node.keys.map(k => {
                const path = node.path.concat([slot, k]);
                const dk = key(path);
                if (k === 'fillpattern') {
                    const sel = h('select', { 'data-key': dk }, [h('option', { value: '', text: ph(k) ? '(' + ph(k) + ')' : '(solid)' })]
                        .concat(node.patterns.map(p => h('option', { value: p, selected: value(k) === p, text: p }))));
                    sel.addEventListener('change', () => sel.value === '' ? clear(path) : edit(path, sel.value));
                    return h('td', {}, [sel]);
                }
                if (k === 'width') {
                    const input = h('input', { type: 'number', step: 'any', 'data-key': dk, value: show(value(k)), placeholder: show(ph(k)) });
                    input.addEventListener('change', () => { const n = numberOf(input); if (n === undefined) clear(path); else if (!isNaN(n)) edit(path, n); });
                    return h('td', {}, [input]);
                }
                if (k === 'dasharray') {
                    const input = h('input', { 'data-key': dk, value: show(value(k)), placeholder: show(ph(k)), size: 6 });
                    input.addEventListener('change', () => input.value.trim() === '' ? clear(path) : edit(path, input.value.trim()));
                    return h('td', {}, [input]);
                }
                return h('td', {}, [colorInput(dk, value(k), ph(k), v => v === '' ? clear(path) : edit(path, v))]);
            });
            const eff = Object.assign({}, inh, own);
            return h('tr', {}, [h('td', { text: slot }), h('td', {}, [swatch(eff)])].concat(cells));
        });
        return h('fieldset', { title: node.description || '' }, [
            h('legend', { text: node.label + (Object.keys(node.own).length ? '' : '  (' + sourceText(node) + ')') }),
            h('table', { class: 'slots' }, [head].concat(rows)),
        ]);
    }

    /** A small bar in the effective look of one slot. */
    function swatch(style) {
        const ns = 'http://www.w3.org/2000/svg';
        const svg = document.createElementNS(ns, 'svg');
        svg.setAttribute('width', '34'); svg.setAttribute('height', '16');
        const id = 'p' + Math.random().toString(36).slice(2);
        const fill = style.fill || '#ffffff';
        if (style.fillpattern === 'hatched' || style.fillpattern === 'fc') {
            const defs = document.createElementNS(ns, 'defs');
            const pat = document.createElementNS(ns, 'pattern');
            pat.setAttribute('id', id); pat.setAttribute('width', '4'); pat.setAttribute('height', '4');
            pat.setAttribute('patternUnits', 'userSpaceOnUse'); pat.setAttribute('patternTransform', 'rotate(45)');
            const line = document.createElementNS(ns, 'rect');
            line.setAttribute('width', '1.5'); line.setAttribute('height', '4'); line.setAttribute('fill', style.stroke || '#404040');
            pat.appendChild(line); defs.appendChild(pat); svg.appendChild(defs);
        }
        const rect = document.createElementNS(ns, 'rect');
        rect.setAttribute('x', '1'); rect.setAttribute('y', '1'); rect.setAttribute('width', '32'); rect.setAttribute('height', '14');
        rect.setAttribute('fill', style.fillpattern === 'hatched' || style.fillpattern === 'fc' ? 'url(#' + id + ')' : (style.fillpattern === 'outlined' ? (style.outlineFill || '#ffffff') : fill));
        rect.setAttribute('stroke', style.fillpattern === 'outlined' ? (style.outlineStroke || style.stroke || '#404040') : (style.stroke || 'none'));
        rect.setAttribute('stroke-width', String(Math.max(1, Number(style.width) * 4 || 1)));
        if (style.dasharray) rect.setAttribute('stroke-dasharray', style.dasharray);
        svg.appendChild(rect);
        return svg;
    }

    // Whole-value writes update the shown value at once, so a second click before the
    // document comes back builds on the first one.
    function writeWhole(node, next, empty) {
        if (empty) { clear(node.path); node.own = undefined; } else { edit(node.path, next); node.own = next; }
        keepFocus(draw);
    }

    function list(node) {
        const own = node.own;
        const items = own || [];
        const write = next => writeWhole(node, next, next.length === 0);
        const rows = items.map((v, i) => h('div', { class: 'list-item' }, [
            node.colors ? colorInput(key(node.path) + '/' + i, v, '', nv => { const next = items.slice(); next[i] = nv; write(next.filter(x => x !== '')); })
                : (function () { const inp = h('input', { 'data-key': key(node.path) + '/' + i, value: show(v) }); inp.addEventListener('change', () => { const next = items.slice(); next[i] = inp.value; write(next.filter(x => x !== '')); }); return inp; })(),
            h('button', { class: 'icon', title: 'Up', text: '\\u2191', onclick: () => { if (i === 0) return; const next = items.slice(); next.splice(i - 1, 0, next.splice(i, 1)[0]); write(next); } }),
            h('button', { class: 'icon', title: 'Down', text: '\\u2193', onclick: () => { if (i === items.length - 1) return; const next = items.slice(); next.splice(i + 1, 0, next.splice(i, 1)[0]); write(next); } }),
            h('button', { class: 'icon', title: 'Remove', text: '\\u00d7', onclick: () => { const next = items.slice(); next.splice(i, 1); write(next); } }),
        ]));
        const inherited = !own && node.inherited && node.inherited.length
            ? h('div', { class: 'muted' }, [sourceText(node) + ': '].concat(node.inherited.map(c => node.colors ? h('span', { class: 'chip', title: show(c), style: 'background:' + show(c) }) : show(c) + ' ')))
            : null;
        const actions = h('div', {}, [
            h('button', { class: 'link', text: '+ add', onclick: () => write(items.concat([node.colors ? '#000000' : 'Series ' + (items.length + 1)])) }),
            !own && node.inherited && node.inherited.length ? h('button', { class: 'link', text: 'override inherited list', onclick: () => write(node.inherited.slice()) }) : null,
        ]);
        return h('fieldset', { title: node.description || '' }, [h('legend', { text: node.label }), inherited].concat(rows).concat([actions]));
    }

    function map(node) {
        const own = node.own || {};
        const write = next => writeWhole(node, next, Object.keys(next).length === 0);
        const rows = Object.entries(own).map(([name, value]) => {
            const nameInput = h('input', { 'data-key': key(node.path) + '/' + name + '/name', value: name, size: 12 });
            nameInput.addEventListener('change', () => {
                const nn = nameInput.value.trim();
                if (!nn || nn === name) return;
                if (Object.prototype.hasOwnProperty.call(own, nn)) { nameInput.value = name; return; }
                const next = {};
                for (const [k, v] of Object.entries(own)) next[k === name ? nn : k] = v;
                write(next);
            });
            return h('div', { class: 'list-item' }, [
                nameInput,
                colorInput(key(node.path) + '/' + name, value, '', v => { const next = Object.assign({}, own); next[name] = v; write(next); }),
                h('button', { class: 'icon', title: 'Remove', text: '\\u00d7', onclick: () => { const next = Object.assign({}, own); delete next[name]; write(next); } }),
            ]);
        });
        const inh = node.inherited && Object.keys(node.inherited).length
            ? h('div', { class: 'muted' }, [sourceText(node) + ': '].concat(Object.entries(node.inherited).map(([n, c]) => h('span', {}, [h('span', { class: 'chip', style: 'background:' + show(c) }), n + ' ']))))
            : null;
        const add = h('button', { class: 'link', text: '+ add', onclick: () => {
            let n = 'color', i = 1;
            while (Object.prototype.hasOwnProperty.call(own, n)) n = 'color' + (++i);
            const next = Object.assign({}, own); next[n] = '#000000'; write(next);
        } });
        return h('fieldset', { title: node.description || '' }, [h('legend', { text: node.label }), inh].concat(rows).concat([add]));
    }

    function render(node) {
        if (node.type === 'leaf') return leaf(node);
        if (node.type === 'group') return group(node);
        if (node.type === 'slots') return slots(node);
        if (node.type === 'list') return list(node);
        return map(node);
    }

    function hasOwn(nodes) {
        return nodes.some(n => n.type === 'group' ? hasOwn(n.children) : n.type === 'slots' ? Object.keys(n.own).length > 0 : n.own !== undefined);
    }

    function draw() {
        const top = document.getElementById('top');
        const main = document.getElementById('main');
        top.replaceChildren();
        main.replaceChildren();
        if (state.empty) { main.appendChild(h('div', { class: 'muted', text: state.empty })); return; }
        const sections = state.sections || [];
        if (!tab || !sections.some(s => s.key === tab)) tab = sections.length ? sections[0].key : undefined;
        vscode.setState({ tab: tab, target: state.current });
        top.appendChild(docPicker(state) || h('span'));
        top.appendChild(h('div', { class: 'header' }, [
            h('h2', { text: state.name }),
            h('span', { class: 'badge', text: 'ComponentStyle' }),
            h('span', { class: 'muted', text: state.isDefault ? 'merged over the engine defaults; every style inherits it'
                : state.hasDefault ? 'merged over _default and the engine defaults' : 'merged over the engine defaults' }),
            h('button', { class: 'link', text: 'Show YAML', onclick: () => post({ type: 'revealSource' }) }),
        ]));
        const banner = jsonBanner(state);
        if (banner) top.appendChild(banner);
        top.appendChild(h('div', { class: 'tabs' }, sections.map(s => h('button', {
            class: 'tab' + (s.key === tab ? ' active' : ''),
            onclick: () => { tab = s.key; keepFocus(draw); },
        }, [s.key, hasOwn(s.nodes) ? h('span', { class: 'dot', text: ' \\u25cf' }) : null]))));
        const section = sections.find(s => s.key === tab);
        const body = h('div', {}, section ? section.nodes.map(render) : []);
        if (state.jsonString) body.querySelectorAll('input,select,button').forEach(el => el.disabled = true);
        main.appendChild(body);
        main.appendChild(usedByList(state, state.isDefault ? 'All components: _default applies to every component.' : ''));
        main.appendChild(h('div', { class: 'muted', text: 'Empty fields show the inherited value. Engine defaults from ' + state.engineVersion + '.' }));
    }

    window.addEventListener('message', event => {
        const msg = event.data;
        if (msg.type === 'state') { state = msg; keepFocus(draw); }
    });
    post({ type: 'ready', target: saved.target });
`;
