import { editorPage } from './editorPage';

/** The page of the RuleSet editor. */
export function getRuleSetEditorHtml(nonce: string): string {
    return editorPage(nonce, 'RuleSet Editor', CSS, SCRIPT);
}

const CSS = `
    table.rules { border-collapse: collapse; margin: 8px 0; }
    table.rules th, table.rules td { padding: 3px 6px; text-align: left; }
    table.rules th { color: var(--vscode-descriptionForeground); font-weight: normal; font-size: 0.85em; border-bottom: 1px solid var(--vscode-panel-border); }
    table.rules tr.dragging { opacity: 0.4; }
    table.rules tr.over td { border-top: 2px solid var(--vscode-focusBorder); }
    table.rules tr.over-after td { border-bottom: 2px solid var(--vscode-focusBorder); }
    td.grip { cursor: grab; color: var(--vscode-descriptionForeground); user-select: none; }
    td.prefix { font-family: var(--vscode-editor-font-family); font-weight: bold; }
    .looks { display: inline-flex; gap: 4px; }
    .look { background: transparent; border: 1px solid transparent; padding: 1px; border-radius: 2px; }
    .look.selected { border-color: var(--vscode-focusBorder); }
    .look.inherited { border-color: var(--vscode-panel-border); border-style: dashed; }
    .look:hover { background: var(--vscode-toolbar-hoverBackground); }
    .set { font-weight: bold; }
    input.name { width: 110px; }
    input[type=number] { width: 70px; }
`;

const SCRIPT = `
    let state;

    function edit(path, value) { post({ type: 'edit', path: path, value: value }); }
    function clear(path) { post({ type: 'remove', path: path }); }

    /** A small bar in one look of the chosen style. */
    function bar(look) {
        const st = look.style || {};
        const ns = 'http://www.w3.org/2000/svg';
        const svg = document.createElementNS(ns, 'svg');
        svg.setAttribute('width', '26'); svg.setAttribute('height', '14');
        const id = 'h' + look.colorIndex;
        const hatched = st.fillpattern === 'hatched' || st.fillpattern === 'fc';
        if (hatched) {
            const defs = document.createElementNS(ns, 'defs');
            const pat = document.createElementNS(ns, 'pattern');
            pat.setAttribute('id', id); pat.setAttribute('width', '4'); pat.setAttribute('height', '4');
            pat.setAttribute('patternUnits', 'userSpaceOnUse'); pat.setAttribute('patternTransform', 'rotate(45)');
            const bg = document.createElementNS(ns, 'rect');
            bg.setAttribute('width', '4'); bg.setAttribute('height', '4'); bg.setAttribute('fill', st.fill || '#ffffff');
            const line = document.createElementNS(ns, 'rect');
            line.setAttribute('width', '1.5'); line.setAttribute('height', '4'); line.setAttribute('fill', st.stroke || '#404040');
            pat.appendChild(bg); pat.appendChild(line); defs.appendChild(pat); svg.appendChild(defs);
        }
        const rect = document.createElementNS(ns, 'rect');
        rect.setAttribute('x', '1'); rect.setAttribute('y', '1'); rect.setAttribute('width', '24'); rect.setAttribute('height', '12');
        const outlined = st.fillpattern === 'outlined';
        rect.setAttribute('fill', hatched ? 'url(#' + id + ')' : outlined ? (st.outlineFill || '#ffffff') : (st.fill || '#ffffff'));
        rect.setAttribute('stroke', outlined ? (st.outlineStroke || st.stroke || '#404040') : (st.stroke || 'none'));
        rect.setAttribute('stroke-width', '1.5');
        if (st.dasharray) rect.setAttribute('stroke-dasharray', st.dasharray);
        svg.appendChild(rect);
        return svg;
    }

    function lookPicker(path, own, inherited) {
        return h('span', { class: 'looks' }, state.looks.map(look => {
            const selected = own === look.colorIndex;
            const cls = 'look' + (selected ? ' selected' : (own === undefined && inherited === look.colorIndex ? ' inherited' : ''));
            return h('button', {
                class: cls, 'data-key': path.join('/') + '/' + look.colorIndex,
                title: look.colorIndex + ': ' + look.label + ' (style slot ' + look.slot + ')' + (selected ? ', click again to inherit' : ''),
                onclick: () => selected ? clear(path) : edit(path, look.colorIndex),
            }, [bar(look)]);
        }));
    }

    function textCell(path, own, inherited, cls) {
        const input = h('input', { class: cls || 'name', 'data-key': path.join('/'), value: show(own), placeholder: show(inherited) });
        input.addEventListener('change', () => input.value.trim() === '' ? clear(path) : edit(path, input.value.trim()));
        return input;
    }

    function numberCell(path, own, inherited) {
        const input = h('input', { type: 'number', 'data-key': path.join('/'), value: show(own), placeholder: show(inherited) });
        input.addEventListener('change', () => {
            const n = numberOf(input);
            if (n === undefined) clear(path);
            else if (!isNaN(n)) edit(path, n);
        });
        return input;
    }

    let dragFrom = -1;
    function scenarioTable() {
        const head = h('tr', {}, ['', 'prefix', 'name', 'look (colorIndex)', 'order (sortIndex)', 'variance order'].map(t => h('th', { text: t })));
        const rows = state.rows.map((row, i) => {
            const base = ['scenarios', row.prefix];
            // only the grip starts a drag: a text drag in an input must not move the row
            const grip = h('td', { class: 'grip', title: 'Drag to change the order', text: '\\u2630' });
            const tr = h('tr', {}, [
                grip,
                h('td', { class: 'prefix' + (Object.keys(row.own).length ? ' set' : ''), text: row.prefix }),
                h('td', {}, [textCell(base.concat(['name']), row.own.name, row.system.name)]),
                h('td', {}, [lookPicker(base.concat(['colorIndex']), row.own.colorIndex, row.system.colorIndex)]),
                h('td', {}, [numberCell(base.concat(['sortIndex']), row.own.sortIndex, row.system.sortIndex)]),
                h('td', { class: 'muted', text: row.variance ? row.variance.absolute + ' / ' + row.variance.relative : '' }),
            ]);
            grip.addEventListener('mousedown', () => { tr.draggable = true; });
            tr.addEventListener('dragstart', e => { dragFrom = i; tr.classList.add('dragging'); e.dataTransfer.effectAllowed = 'move'; });
            tr.addEventListener('dragend', () => {
                tr.draggable = false;
                dragFrom = -1;
                tr.classList.remove('dragging');
                document.querySelectorAll('tr.over, tr.over-after').forEach(r => r.classList.remove('over', 'over-after'));
            });
            // the row goes where the line is: above the target when moved up, below it when moved down
            tr.addEventListener('dragover', e => { if (dragFrom < 0) return; e.preventDefault(); tr.classList.add(dragFrom < i ? 'over-after' : 'over'); });
            tr.addEventListener('dragleave', () => tr.classList.remove('over', 'over-after'));
            tr.addEventListener('drop', e => {
                e.preventDefault();
                if (dragFrom < 0 || dragFrom === i) return;
                const order = state.rows.map(r => r.prefix);
                const moved = order.splice(dragFrom, 1)[0];
                order.splice(i, 0, moved);
                post({ type: 'reorder', order: order });
            });
            return tr;
        });
        return h('table', { class: 'rules' }, [head].concat(rows));
    }

    function fallbackTable() {
        const f = state.fallback;
        const base = ['fallback'];
        return h('table', { class: 'rules' }, [
            h('tr', {}, ['name', 'group', 'look (colorIndex)', 'order (sortIndex)'].map(t => h('th', { text: t }))),
            h('tr', {}, [
                h('td', {}, [textCell(base.concat(['name']), f.own.name, f.system.name)]),
                h('td', {}, [textCell(base.concat(['group']), f.own.group, f.system.group)]),
                h('td', {}, [lookPicker(base.concat(['colorIndex']), f.own.colorIndex, f.system.colorIndex)]),
                h('td', {}, [numberCell(base.concat(['sortIndex']), f.own.sortIndex, f.system.sortIndex)]),
            ]),
        ]);
    }

    function draw() {
        const top = document.getElementById('top');
        const main = document.getElementById('main');
        top.replaceChildren();
        main.replaceChildren();
        if (state.empty) { main.appendChild(h('div', { class: 'muted', text: state.empty })); return; }
        vscode.setState({ target: state.current });
        top.appendChild(docPicker(state) || h('span'));
        const styleSel = h('select', { onchange: () => post({ type: 'pickStyle', name: styleSel.value }) },
            state.styles.map(n => h('option', { value: n, selected: n === state.style, text: n === '_system' ? '_system (engine defaults)' : n })));
        top.appendChild(h('div', { class: 'header' }, [
            h('h2', { text: state.name }),
            h('span', { class: 'badge', text: 'RuleSet' }),
            h('span', { class: 'muted', text: 'Looks drawn in style' }),
            styleSel,
            h('button', { class: 'link', text: 'Show YAML', onclick: () => post({ type: 'revealSource' }) }),
        ]));
        const banner = jsonBanner(state);
        if (banner) top.appendChild(banner);
        const body = h('div', {}, [
            h('h3', { text: 'Scenarios' }),
            h('div', { class: 'muted', text: 'Grey values are the engine defaults. Rows are in column order; drag a row to move it (sortIndex is written in steps of 100).' }),
            scenarioTable(),
            h('h3', { text: 'Fallback' }),
            h('div', { class: 'muted', text: 'For scenario names that no prefix above matches.' }),
            fallbackTable(),
        ]);
        if (state.jsonString) body.querySelectorAll('input,select,button').forEach(el => el.disabled = true);
        main.appendChild(body);
        main.appendChild(usedByList(state, ''));
        main.appendChild(h('div', { class: 'muted', text: 'A page or card that names this RuleSet also passes it to children with ruleset: inherited-closest or inherited-page.' }));
    }

    window.addEventListener('message', event => {
        const msg = event.data;
        if (msg.type === 'state') { state = msg; keepFocus(draw); }
    });
    post({ type: 'ready', target: saved.target });
`;
