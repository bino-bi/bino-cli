import { editorPage } from '../styles/editorPage';

/** The page of the annotation form. */
export function getAnnotationFormHtml(nonce: string): string {
    return editorPage(nonce, 'Annotation', CSS, SCRIPT);
}

const CSS = `
    .row { display: grid; grid-template-columns: 150px 1fr; gap: 8px; align-items: center; padding: 3px 0; }
    fieldset { border: 1px solid var(--vscode-panel-border); margin: 8px 0; padding: 4px 10px 8px; }
    legend { padding: 0 4px; }
    textarea { width: 100%; min-height: 60px; background: var(--vscode-input-background); color: var(--vscode-input-foreground, var(--vscode-foreground)); border: 1px solid var(--vscode-input-border, transparent); font-family: inherit; }
    select.component { min-width: 260px; }
    input.point { width: 220px; }
    .hint { font-size: 0.85em; color: var(--vscode-descriptionForeground); margin-left: 6px; }
    .error { color: var(--vscode-errorForeground); }
    .actions { margin-top: 12px; display: flex; gap: 8px; align-items: center; }
    .waiting { outline: 2px solid var(--vscode-focusBorder); }
`;

const SCRIPT = `
    let state;
    let spec = {};
    let name = '';
    let waiting;
    const valueCache = {};
    const FIELD_HINT = {
        rowGroup: 'raw value of the rowGroup column', category: 'raw value of the category column',
        subCategory: 'raw value of the subCategory column', date: 'first day of the period, YYYY-MM-DD',
        scenario: 'scenario column, e.g. ac1', row: 'subtotal or total row', part: 'element (default), label or total',
    };

    function applies(key) {
        const only = state.markFields[key];
        return !only || only.includes(spec.mark || 'marker');
    }
    function componentOf(end) {
        const t = spec[end] || {};
        return state.components.find(c => c.kind === t.kind && c.name === t.name);
    }
    function row(label, control, hint) {
        return h('div', { class: 'row' }, [h('label', { text: label }), h('span', {}, [control, hint ? h('span', { class: 'hint', text: hint }) : null])]);
    }
    function setSpec(key, value) {
        if (value === '' || value === undefined || value === null) delete spec[key]; else spec[key] = value;
    }

    function endPicker(end) {
        const t = spec[end] = spec[end] || {};
        const comp = componentOf(end);
        const sel = h('select', { class: 'component', 'data-key': end + '/component' },
            [h('option', { value: '', text: '(choose a component)' })].concat(state.components.map(c => h('option', {
                value: c.kind + ':' + c.name, selected: comp === c, text: c.kind + '  ' + c.name,
            }))));
        sel.addEventListener('change', () => {
            const [kind, ...rest] = sel.value.split(':');
            spec[end] = sel.value ? { kind: kind, name: rest.join(':') } : {};
            keepFocus(draw);
        });
        const parts = [row('component', sel, comp && comp.fields.length === 0 && comp.kind !== 'SmallMultiple' ? 'no data points: the mark goes on the whole component' : '')];
        if (comp && comp.kind === 'SmallMultiple') {
            const panel = h('input', { 'data-key': end + '/panel', value: t.panel || '', placeholder: 'label of one panel' });
            panel.addEventListener('change', () => { if (panel.value.trim()) t.panel = panel.value.trim(); else delete t.panel; keepFocus(draw); });
            parts.push(row('panel', panel, t.panel ? '' : 'empty: the whole SmallMultiple'));
        }
        // a difference always means the bars: it has no part
        const fields = (comp ? (comp.kind === 'SmallMultiple' && !t.panel ? [] : comp.fields) : [])
            .filter(f => !(f === 'part' && spec.mark === 'difference'));
        for (const field of fields) {
            const key = end + '/' + field;
            const listId = 'list-' + end + '-' + field;
            const input = h('input', { class: 'point', 'data-key': key, list: listId, value: (t.point || {})[field] || '', placeholder: 'any' });
            const list = h('datalist', { id: listId });
            const cacheKey = comp.kind + ':' + comp.name + ':' + field;
            const fill = values => { list.replaceChildren(...values.map(v => h('option', { value: v }))); };
            const status = h('span', { class: 'hint' });
            if (valueCache[cacheKey]) {
                fill(valueCache[cacheKey].values);
                if (valueCache[cacheKey].error) { status.className = 'hint error'; status.textContent = valueCache[cacheKey].error; }
                else if (valueCache[cacheKey].truncated) { status.textContent = 'first ' + valueCache[cacheKey].values.length + ' values; type to search'; }
            } else {
                valueCache[cacheKey] = { values: [], pending: true };
                post({ type: 'values', key: cacheKey, kind: comp.kind, name: comp.name, field: field });
            }
            input.addEventListener('change', () => {
                t.point = t.point || {};
                if (input.value.trim() === '') delete t.point[field]; else t.point[field] = input.value.trim();
            });
            parts.push(h('div', { class: 'row' }, [h('label', { text: field, title: FIELD_HINT[field] || '' }), h('span', {}, [input, list, status])]));
        }
        if (comp && fields.length) {
            parts.push(h('div', { class: 'hint', text: 'Raw dataset values. A field left empty matches any value; the point must match exactly one element.' }));
        }
        const pick = state.canPick ? h('button', {
            class: 'link', text: waiting === end ? 'Click a data point in the preview...' : 'Pick in preview',
            onclick: () => { waiting = waiting === end ? undefined : end; post({ type: 'waiting', end: waiting }); keepFocus(draw); },
        }) : null;
        const title = end === 'target' ? 'Target' : 'To' + (state.toRequired.includes(spec.mark) ? ' (required)' : ' (optional: a frame around two points)');
        return h('fieldset', { class: waiting === end ? 'waiting' : '' }, [h('legend', { text: title }), pick].concat(parts));
    }

    function draw() {
        const top = document.getElementById('top');
        const main = document.getElementById('main');
        top.replaceChildren();
        main.replaceChildren();
        const nameInput = h('input', { 'data-key': 'name', value: name, disabled: state.mode === 'edit', size: 30 });
        nameInput.addEventListener('change', () => { name = nameInput.value.trim(); });
        top.appendChild(h('div', { class: 'header' }, [
            h('h2', { text: state.mode === 'edit' ? 'Annotation' : 'New annotation' }),
            nameInput,
            state.mode === 'edit' ? h('button', { class: 'link', text: 'Show YAML', onclick: () => post({ type: 'reveal' }) }) : null,
        ]));

        const mark = h('select', { 'data-key': 'mark' }, state.marks.map(m => h('option', { value: m, selected: (spec.mark || 'marker') === m, text: m })));
        mark.addEventListener('change', () => { setSpec('mark', mark.value); keepFocus(draw); });
        const color = h('select', { 'data-key': 'color' }, [h('option', { value: '', text: '(main colour)' })]
            .concat(state.colors.map(c => h('option', { value: c, selected: spec.color === c, text: c })))
            .concat(spec.color && !state.colors.includes(spec.color) ? [h('option', { value: spec.color, selected: true, text: spec.color + ' (not in the style)' })] : []));
        color.addEventListener('change', () => setSpec('color', color.value));

        const body = [row('mark', mark), row('color', color, 'a name from bn-annotation.colors of the style'), endPicker('target')];
        if (applies('to')) body.push(endPicker('to')); else delete spec.to;

        const text = (key, label, hint) => {
            const input = h('input', { 'data-key': key, value: spec[key] || '', size: 40 });
            input.addEventListener('change', () => setSpec(key, input.value.trim()));
            return row(label, input, hint);
        };
        const check = (key, label, hint) => {
            const input = h('input', { type: 'checkbox', 'data-key': key, checked: spec[key] === true });
            input.addEventListener('change', () => setSpec(key, input.checked || undefined));
            return row(label, input, hint);
        };
        const choice = (key, options, hint) => {
            const sel = h('select', { 'data-key': key }, [h('option', { value: '', text: '(default)' })].concat(options.map(o => h('option', { value: o, selected: spec[key] === o, text: o }))));
            sel.addEventListener('change', () => setSpec(key, sel.value));
            return row(key, sel, hint);
        };
        if (applies('markText')) body.push(text('markText', 'markText', spec.mark === 'bracket' ? 'text of the bracket' : 'text printed on the target, inside the frame'));
        if (applies('markTextPosition')) body.push(choice('markTextPosition', ['beside', 'above'], 'for a frame on a value label'));
        if (applies('percent')) body.push(check('percent', 'percent', spec.mark === 'bracket' ? 'print the share of the rows' : 'also print the relative difference'));
        if (applies('digits')) {
            const digits = h('input', { type: 'number', min: 0, max: 6, 'data-key': 'digits', value: spec.digits === undefined ? '' : spec.digits, placeholder: '1' });
            digits.addEventListener('change', () => { const n = numberOf(digits); if (n === undefined) delete spec.digits; else if (!isNaN(n)) spec.digits = n; });
            body.push(row('digits', digits));
        }
        if (applies('sentiment')) body.push(choice('sentiment', ['good-up', 'good-down']));
        if (applies('highlight')) body.push(check('highlight', 'highlight', 'frame around the relative difference'));
        body.push(text('title', 'title', 'bold in the AnnotationList'));
        const area = h('textarea', { 'data-key': 'text' });
        area.value = spec.text || '';
        area.defaultValue = area.value;
        area.addEventListener('change', () => setSpec('text', area.value.trim()));
        body.push(row('text', area, 'inline Markdown'));

        const save = h('button', { text: state.mode === 'edit' ? 'Save' : 'Create', onclick: () => {
            // a change event of the focused field has not fired yet
            if (document.activeElement && document.activeElement.blur) document.activeElement.blur();
            post({ type: 'save', name: name, spec: spec });
        } });
        main.appendChild(h('div', {}, body));
        main.appendChild(h('div', { class: 'actions' }, [save]));
    }

    window.addEventListener('message', event => {
        const msg = event.data;
        if (msg.type === 'state') {
            state = msg;
            spec = JSON.parse(JSON.stringify(msg.spec || {}));
            name = msg.name || '';
            waiting = undefined;
            draw();
        } else if (msg.type === 'values') {
            valueCache[msg.key] = msg;
            keepFocus(draw);
        } else if (msg.type === 'picked') {
            spec[msg.end] = msg.target;
            waiting = undefined;
            // an arrow, a difference or a bracket needs a second point: the next click gives it
            if (msg.end === 'target' && state.toRequired.includes(spec.mark) && !(spec.to && spec.to.kind)) {
                waiting = 'to';
                post({ type: 'waiting', end: 'to' });
            }
            keepFocus(draw);
        } else if (msg.type === 'canPick') {
            state.canPick = msg.on;
            if (!msg.on && waiting) { waiting = undefined; post({ type: 'waiting' }); }
            keepFocus(draw);
        }
    });
    post({ type: 'ready' });
`;
