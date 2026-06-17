function getNonce(): string {
    let text = '';
    const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
    for (let i = 0; i < 32; i++) {
        text += chars.charAt(Math.floor(Math.random() * chars.length));
    }
    return text;
}

/** Returns the full HTML for the wizard webview. The webview is self-contained:
 *  it holds the editable state and talks to the host via postMessage. */
export function getWizardHtml(): string {
    const nonce = getNonce();
    const csp = `default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';`;
    return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta http-equiv="Content-Security-Policy" content="${csp}">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<style>${styles()}</style>
</head>
<body>
<div id="app">
  <header>
    <h1 id="title">New DataSource</h1>
    <span id="busy" class="busy hidden">working…</span>
  </header>
  <section id="source-options" class="card"></section>
  <div id="error" class="error hidden"></div>
  <section id="schema" class="hidden">
    <div class="card">
      <div class="card-title">Source preview <span id="preview-note" class="muted"></span></div>
      <div id="preview" class="scroll"></div>
    </div>
  </section>
  <footer id="footer" class="hidden">
    <div class="row">
      <label>DataSource name <input id="ds-name" type="text"></label>
    </div>
    <div class="row">
      <label class="check"><input id="create-dataset" type="checkbox" checked> Create matching DataSet</label>
      <label id="dataset-name-wrap">DataSet name <input id="dataset-name" type="text"></label>
    </div>
    <div id="dataset-section">
      <div class="card">
        <div class="card-title">Map to the dataset schema <span class="muted">— fill each dataset column from a source column, a constant, or an expression</span></div>
        <div id="columns" class="scroll"></div>
        <div id="map-warn" class="warn hidden"></div>
        <div class="row"><button id="add-custom-col" type="button">+ Add custom (_) column</button></div>
      </div>
      <div class="card" id="sql-card">
        <div class="card-title">Generated DataSet SQL <span class="muted">— read-only; edit the mapping above to change it</span></div>
        <pre id="sql" class="sql"></pre>
        <div class="row">
          <button id="preview-dataset" type="button">▶ Preview dataset</button>
          <span id="dataset-preview-note" class="muted"></span>
        </div>
      </div>
      <div class="card hidden" id="dataset-preview-card">
        <div class="card-title">Dataset preview</div>
        <div id="dataset-preview" class="scroll"></div>
      </div>
    </div>
    <div class="row actions">
      <button id="create" class="primary">Create</button>
    </div>
  </footer>
</div>
<script nonce="${nonce}">${script()}</script>
</body>
</html>`;
}

function styles(): string {
    return `
  * { box-sizing: border-box; }
  body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); padding: 12px; font-size: var(--vscode-font-size); }
  header { display: flex; align-items: center; gap: 12px; margin-bottom: 10px; }
  h1 { font-size: 1.2em; margin: 0; }
  .busy { color: var(--vscode-descriptionForeground); font-style: italic; }
  .hidden { display: none !important; }
  .muted, .card-title .muted { color: var(--vscode-descriptionForeground); font-weight: normal; }
  .card { border: 1px solid var(--vscode-panel-border); border-radius: 4px; padding: 10px; margin-bottom: 12px; }
  .card-title { font-weight: 600; margin-bottom: 8px; }
  .scroll { max-height: 320px; overflow: auto; }
  label { display: inline-flex; flex-direction: column; gap: 3px; font-size: 0.92em; margin-right: 14px; }
  label.check, label.cast { flex-direction: row; align-items: center; gap: 6px; }
  input[type=text], input[type=number], select, textarea {
    background: var(--vscode-input-background); color: var(--vscode-input-foreground);
    border: 1px solid var(--vscode-input-border, transparent); border-radius: 3px; padding: 4px 6px; font-family: inherit; font-size: inherit;
  }
  textarea { width: 100%; min-height: 70px; font-family: var(--vscode-editor-font-family); }
  input[type=text].small, input[type=number] { width: 90px; }
  .row { display: flex; flex-wrap: wrap; align-items: end; gap: 10px; margin-bottom: 8px; }
  button { background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); border: none; border-radius: 3px; padding: 5px 12px; cursor: pointer; }
  button.primary { background: var(--vscode-button-background); color: var(--vscode-button-foreground); }
  button:hover { opacity: 0.9; }
  table { border-collapse: collapse; width: 100%; font-size: 0.88em; }
  th, td { border: 1px solid var(--vscode-panel-border); padding: 3px 6px; text-align: left; white-space: nowrap; }
  th { position: sticky; top: 0; background: var(--vscode-editor-background); }
  .col-table { width: 100%; table-layout: fixed; }
  .col-table th, .col-table td { white-space: normal; word-break: break-word; vertical-align: middle; }
  .col-table th:nth-child(1), .col-table td:nth-child(1) { width: 26%; }
  .col-table th:nth-child(2), .col-table td:nth-child(2) { width: 18%; }
  .col-table th:nth-child(4), .col-table td:nth-child(4) { width: 20%; }
  .col-table input[type=text], .col-table select { width: 100%; min-width: 60px; }
  .col-table tr.grp td { font-weight: 600; background: var(--vscode-editorWidget-background, rgba(127,127,127,0.10)); color: var(--vscode-foreground); }
  .col-table .m-del { color: var(--vscode-errorForeground); margin-left: 4px; }
  .type { color: var(--vscode-descriptionForeground); font-family: var(--vscode-editor-font-family); }
  pre.sql { margin: 0; max-height: 260px; padding: 10px; border-radius: 3px; overflow: auto;
    background: var(--vscode-textCodeBlock-background, var(--vscode-input-background)); color: var(--vscode-foreground);
    border: 1px solid var(--vscode-input-border, transparent); font-family: var(--vscode-editor-font-family);
    white-space: pre; line-height: 1.45; }
  button.link { background: none; border: none; color: var(--vscode-textLink-foreground); padding: 0; font-size: 0.85em; cursor: pointer; }
  button.link:hover { text-decoration: underline; opacity: 1; }
  .m-namewrap { display: flex; align-items: center; gap: 4px; }
  .m-namewrap .m-name { flex: 1; min-width: 0; width: auto; }
  .m-namewrap .m-del { flex: none; }
  .error { background: var(--vscode-inputValidation-errorBackground); border: 1px solid var(--vscode-inputValidation-errorBorder); padding: 8px; border-radius: 3px; margin-bottom: 12px; }
  .warn { background: var(--vscode-inputValidation-warningBackground); border: 1px solid var(--vscode-inputValidation-warningBorder); color: var(--vscode-foreground); padding: 6px 8px; border-radius: 3px; margin-top: 8px; font-size: 0.9em; }
  .actions { justify-content: flex-end; }
  `;
}

function script(): string {
    return `
  const vscode = acquireVsCodeApi();
  const TYPES = ['VARCHAR','BIGINT','INTEGER','DOUBLE','DECIMAL(18,2)','DECIMAL(10,2)','DATE','TIMESTAMP','TIME','BOOLEAN'];
  // Fallback dataset schema (raw: name/kind/group/pair) mirroring
  // internal/report/dataset/schema.go. The live schema fetched from the CLI/daemon
  // (init.datasetSchema) overrides it, so the editor never drifts from the CLI.
  const MEASURE_COLS = ['ac1','ac2','ac3','ac4','pp1','pp2','pp3','pp4','fc1','fc2','fc3','fc4','pl1','pl2','pl3','pl4'];
  const RAW_DEFAULT = MEASURE_COLS.map(n => ({ name: n, kind: 'number', group: 'Measures' })).concat([
    { name: 'rowGroup', kind: 'string', group: 'Dimensions', pair: 'rowGroupIndex' },
    { name: 'rowGroupIndex', kind: 'number', group: 'Dimensions', pair: 'rowGroup' },
    { name: 'category', kind: 'string', group: 'Dimensions', pair: 'categoryIndex' },
    { name: 'categoryIndex', kind: 'number', group: 'Dimensions', pair: 'category' },
    { name: 'subCategory', kind: 'string', group: 'Dimensions', pair: 'subCategoryIndex' },
    { name: 'subCategoryIndex', kind: 'number', group: 'Dimensions', pair: 'subCategory' },
    { name: 'columnGroup', kind: 'string', group: 'Dimensions', pair: 'columnGroupIndex' },
    { name: 'columnGroupIndex', kind: 'number', group: 'Dimensions', pair: 'columnGroup' },
    { name: 'columnSubGroup', kind: 'string', group: 'Dimensions', pair: 'columnSubGroupIndex' },
    { name: 'columnSubGroupIndex', kind: 'number', group: 'Dimensions', pair: 'columnSubGroup' },
    { name: 'date', kind: 'string', group: 'Metadata' },
    { name: 'operation', kind: 'string', group: 'Metadata' },
    { name: 'setname', kind: 'string', group: 'Metadata' },
  ]);
  let DS_COLUMNS = [], DS_GROUPS = [], PAIRS = {};
  function applySchema(raw) {
    const cols = (raw && raw.length) ? raw : RAW_DEFAULT;
    DS_COLUMNS = cols.map(c => ({
      name: c.name, group: c.group, type: c.kind,
      cast: c.kind === 'number' ? (c.group === 'Measures' ? 'DOUBLE' : 'INTEGER') : 'VARCHAR',
      pair: c.pair || '',
    }));
    PAIRS = {};
    DS_GROUPS = [];
    DS_COLUMNS.forEach(c => { if (c.pair) { PAIRS[c.name] = c.pair; } if (DS_GROUPS.indexOf(c.group) < 0) { DS_GROUPS.push(c.group); } });
    DS_GROUPS.push('Custom');
  }
  applySchema(null);
  let state = { source: null, sourceColumns: [], mapping: [], sampleRows: [], sheets: [], dataSourceName: '', dataSetName: '', secrets: [], sampleRowLimit: 100, detectedApplied: false };
  let sqlTimer = null;

  const $ = (id) => document.getElementById(id);

  window.addEventListener('message', (e) => {
    const m = e.data;
    if (m.type === 'init') {
      applySchema(m.datasetSchema);
      state.source = m.source;
      state.dataSourceName = m.dataSourceName;
      state.dataSetName = m.dataSetName;
      state.secrets = m.secrets || [];
      state.sampleRowLimit = m.sampleRowLimit || 100;
      state.detectedApplied = false;
      $('title').textContent = m.source.kind === 'file' ? ('DataSource from ' + m.source.fileName) : 'New database DataSource';
      renderSourceOptions();
      renderFooter();
      if (m.source.kind === 'file') { introspect(); }
    } else if (m.type === 'introspectResult') {
      onIntrospect(m.result);
    } else if (m.type === 'sql') {
      $('sql').textContent = m.sql;
    } else if (m.type === 'datasetPreview') {
      renderDatasetPreview(m);
    } else if (m.type === 'busy') {
      $('busy').classList.toggle('hidden', !m.busy);
    } else if (m.type === 'created') {
      if (m.error) { showError(m.error); }
    }
  });

  function showError(msg) { const el = $('error'); el.textContent = msg; el.classList.remove('hidden'); }
  function clearError() { $('error').classList.add('hidden'); }

  function esc(s) { return String(s ?? '').replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])); }

  // Format a scanned cell for display. DuckDB DECIMAL arrives as {Width,Scale,Value};
  // render it as a fixed-point number instead of "[object Object]".
  function fmtCell(v) {
    if (v === null || v === undefined) { return ''; }
    if (typeof v === 'object') {
      if (typeof v.Value === 'number' && typeof v.Scale === 'number') {
        return (v.Value / Math.pow(10, v.Scale)).toFixed(v.Scale);
      }
      return JSON.stringify(v);
    }
    return String(v);
  }

  function renderSourceOptions() {
    const s = state.source;
    const el = $('source-options');
    if (s.kind === 'file') {
      let opts = '';
      if (s.format === 'csv') {
        opts = \`
          <label>Delimiter <input id="o-delim" type="text" class="small" value="\${esc(s.delimiter || '')}" placeholder="auto"></label>
          <label class="check"><input id="o-header" type="checkbox" \${s.header !== false ? 'checked' : ''}> Header row</label>
          <label>Skip rows <input id="o-skip" type="number" min="0" value="\${s.skipRows || 0}"></label>
          <button id="reintrospect">Re-introspect</button>\`;
      } else if (s.format === 'excel') {
        const sheetOpts = (state.sheets || []).map(sh => \`<option value="\${esc(sh)}" \${sh === s.sheet ? 'selected' : ''}>\${esc(sh)}</option>\`).join('');
        opts = \`<label>Sheet <select id="o-sheet">\${sheetOpts || '<option value="">(default)</option>'}</select></label>
          <button id="reintrospect">Re-introspect</button>\`;
      } else {
        opts = '<span class="muted">Parquet schema is read directly — no options.</span>';
      }
      el.innerHTML = '<div class="card-title">File: ' + esc(s.fileName) + ' <span class="muted">(' + s.format + ')</span></div><div class="row">' + opts + '</div>';
    } else {
      const secretOpts = ['<option value="">(select secret)</option>'].concat(state.secrets.map(x => \`<option value="\${esc(x)}" \${x === s.connection.secret ? 'selected' : ''}>\${esc(x)}</option>\`)).join('');
      el.innerHTML = \`
        <div class="card-title">Database connection</div>
        <div class="row">
          <label>Type <select id="db-type">
            <option value="postgres_query" \${s.dbType==='postgres_query'?'selected':''}>PostgreSQL</option>
            <option value="mysql_query" \${s.dbType==='mysql_query'?'selected':''}>MySQL</option>
          </select></label>
          <label>Host <input id="db-host" type="text" value="\${esc(s.connection.host)}"></label>
          <label>Port <input id="db-port" type="number" value="\${s.connection.port}"></label>
          <label>Database <input id="db-database" type="text" value="\${esc(s.connection.database)}"></label>
          <label>User <input id="db-user" type="text" value="\${esc(s.connection.user)}"></label>
          <label>Secret <select id="db-secret">\${secretOpts}</select></label>
        </div>
        <div class="row"><label style="width:100%">Query <textarea id="db-query" placeholder="SELECT * FROM ...">\${esc(s.query)}</textarea></label></div>
        <div class="row"><button id="reintrospect" class="primary">Introspect</button></div>\`;
    }
    const btn = $('reintrospect');
    if (btn) { btn.addEventListener('click', () => { readSourceOptions(); introspect(); }); }
  }

  function readSourceOptions() {
    const s = state.source;
    if (s.kind === 'file') {
      if (s.format === 'csv') {
        s.delimiter = $('o-delim').value.trim();
        s.header = $('o-header').checked;
        s.skipRows = parseInt($('o-skip').value, 10) || 0;
      } else if (s.format === 'excel' && $('o-sheet')) {
        s.sheet = $('o-sheet').value;
      }
    } else {
      s.dbType = $('db-type').value;
      s.connection.host = $('db-host').value.trim();
      s.connection.port = parseInt($('db-port').value, 10) || 0;
      s.connection.database = $('db-database').value.trim();
      s.connection.user = $('db-user').value.trim();
      s.connection.secret = $('db-secret').value;
      s.query = $('db-query').value;
    }
  }

  function introspect() { clearError(); vscode.postMessage({ type: 'introspect', source: state.source }); }

  function onIntrospect(result) {
    if (result.error) { showError(result.error); return; }
    clearError();
    // Apply detected CSV options once (don't clobber user edits).
    if (!state.detectedApplied && state.source.kind === 'file' && state.source.format === 'csv' && result.detectedCsv) {
      const d = result.detectedCsv;
      if (!state.source.delimiter && d.delimiter) { state.source.delimiter = d.delimiter; }
      if (typeof d.hasHeader === 'boolean') { state.source.header = d.hasHeader; }
      state.detectedApplied = true;
      renderSourceOptions();
    }
    if (result.sheets && result.sheets.length) {
      state.sheets = result.sheets;
      if (state.source.kind === 'file' && state.source.format === 'excel' && !state.source.sheet) {
        state.source.sheet = result.sheets[0];
      }
      renderSourceOptions();
    }
    state.sampleRows = result.sampleRows || [];
    state.sourceColumns = (result.columns || []).map(c => ({ name: c.name, type: c.type }));
    state.mapping = buildMapping(state.sourceColumns);
    // Fresh schema -> regenerate SQL from scratch, discarding any stale preview.
    $('dataset-preview-card').classList.add('hidden');
    $('dataset-preview-note').textContent = '';
    $('schema').classList.remove('hidden');
    $('footer').classList.remove('hidden');
    renderPreview(result.truncated);
    renderMapper();
    requestSql();
  }

  // Build the target-driven mapping: every standard dataset column, with a source
  // pre-filled when its name matches a source column; plus pass-through rows for
  // custom (_) source columns that aren't a standard target.
  function buildMapping(sourceCols) {
    const byLower = {}, predefined = {};
    sourceCols.forEach(c => { byLower[c.name.toLowerCase()] = c.name; });
    DS_COLUMNS.forEach(d => { predefined[d.name.toLowerCase()] = true; });
    const mapping = DS_COLUMNS.map(dc => {
      const m = { target: dc.name, group: dc.group, expectedType: dc.type, defaultCast: dc.cast, custom: false, kind: 'none', source: '', cast: dc.cast, constant: '', expr: '' };
      const match = byLower[dc.name.toLowerCase()];
      if (match) { m.kind = 'source'; m.source = match; }
      return m;
    });
    sourceCols.forEach(c => {
      if (c.name.charAt(0) === '_' && !predefined[c.name.toLowerCase()]) {
        mapping.push({ target: c.name, group: 'Custom', expectedType: 'string', defaultCast: '', custom: true, kind: 'source', source: c.name, cast: '', constant: '', expr: '' });
      }
    });
    return mapping;
  }

  function renderPreview(truncated) {
    const cols = state.sourceColumns.map(c => c.name);
    $('preview-note').textContent = state.sampleRows.length ? ('· ' + state.sampleRows.length + ' rows' + (truncated ? '+' : '')) : '';
    if (!cols.length) { $('preview').innerHTML = '<div class="muted">No columns.</div>'; return; }
    let html = '<table><thead><tr>' + cols.map(c => '<th>' + esc(c) + '</th>').join('') + '</tr></thead><tbody>';
    for (const row of state.sampleRows) {
      html += '<tr>' + cols.map(c => '<td>' + esc(fmtCell(row[c])) + '</td>').join('') + '</tr>';
    }
    html += '</tbody></table>';
    $('preview').innerHTML = html;
  }

  function kindOptions(sel) {
    return [['none','—'],['source','Source column'],['const','Constant'],['expr','Expression']]
      .map(o => '<option value="' + o[0] + '"' + (o[0] === sel ? ' selected' : '') + '>' + o[1] + '</option>').join('');
  }

  function mapperRow(m, i) {
    const badge = m.custom ? '' : ' <span class="type">' + (m.expectedType === 'number' ? 'num' : 'str') + '</span>';
    let nameCell;
    if (m.custom) {
      nameCell = '<span class="m-namewrap"><input type="text" class="m-name" data-i="' + i + '" value="' + esc(m.target) + '">'
        + '<button type="button" class="link m-del" data-i="' + i + '" title="Remove column">✕</button></span>';
    } else {
      nameCell = esc(m.target) + badge;
    }
    let valueCell, castCell = '';
    if (m.kind === 'source') {
      const opts = ['<option value="">(pick column)</option>'].concat(state.sourceColumns.map(c =>
        '<option value="' + esc(c.name) + '"' + (c.name === m.source ? ' selected' : '') + '>' + esc(c.name) + '</option>')).join('');
      valueCell = '<select class="m-source" data-i="' + i + '">' + opts + '</select>';
      const castOpts = ['<option value="">(keep as-is)</option>'].concat(TYPES.map(t =>
        '<option value="' + esc(t) + '"' + (t === m.cast ? ' selected' : '') + '>' + esc(t) + '</option>')).join('');
      castCell = '<select class="m-cast" data-i="' + i + '">' + castOpts + '</select>';
    } else if (m.kind === 'const') {
      const ph = m.expectedType === 'number' ? 'e.g. 0' : 'e.g. +';
      valueCell = '<input type="text" class="m-const" data-i="' + i + '" value="' + esc(m.constant) + '" placeholder="' + ph + '">';
    } else if (m.kind === 'expr') {
      valueCell = '<input type="text" class="m-expr" data-i="' + i + '" value="' + esc(m.expr) + '" placeholder="e.g. sum(amount)">';
    } else {
      valueCell = '<span class="muted">not mapped</span>';
    }
    return '<tr><td>' + nameCell + '</td>'
      + '<td><select class="m-kind" data-i="' + i + '">' + kindOptions(m.kind) + '</select></td>'
      + '<td>' + valueCell + '</td><td>' + castCell + '</td></tr>';
  }

  function renderMapper() {
    let html = '<table class="col-table"><thead><tr><th>Dataset column</th><th>From</th><th>Value</th><th>Cast</th></tr></thead><tbody>';
    DS_GROUPS.forEach(group => {
      const idx = [];
      state.mapping.forEach((m, i) => { if (m.group === group) { idx.push(i); } });
      if (!idx.length) { return; }
      html += '<tr class="grp"><td colspan="4">' + esc(group) + '</td></tr>';
      idx.forEach(i => { html += mapperRow(state.mapping[i], i); });
    });
    html += '</tbody></table>';
    $('columns').innerHTML = html;
    wireMapper();
    updateWarnings();
  }

  // Warn when a dimension is mapped without its required index partner (or vice versa).
  function updateWarnings() {
    const el = $('map-warn');
    if (!el) { return; }
    const cols = mappedColumns();
    const have = {};
    cols.forEach(c => { have[c.alias] = true; });
    const missing = [];
    cols.forEach(c => { const p = PAIRS[c.alias]; if (p && !have[p]) { missing.push(c.alias + ' → add ' + p); } });
    if (missing.length) {
      el.textContent = '⚠ Pair these dimensions with their index column: ' + missing.join('; ') + '.';
      el.classList.remove('hidden');
    } else {
      el.classList.add('hidden');
    }
  }

  function wireMapper() {
    const root = $('columns');
    root.querySelectorAll('.m-kind').forEach(el => el.addEventListener('change', e => { state.mapping[+e.target.dataset.i].kind = e.target.value; renderMapper(); requestSql(); }));
    root.querySelectorAll('.m-source').forEach(el => el.addEventListener('change', e => { state.mapping[+e.target.dataset.i].source = e.target.value; requestSql(); }));
    root.querySelectorAll('.m-cast').forEach(el => el.addEventListener('change', e => { state.mapping[+e.target.dataset.i].cast = e.target.value; requestSql(); }));
    root.querySelectorAll('.m-const').forEach(el => el.addEventListener('input', e => { state.mapping[+e.target.dataset.i].constant = e.target.value; requestSql(); }));
    root.querySelectorAll('.m-expr').forEach(el => el.addEventListener('input', e => { state.mapping[+e.target.dataset.i].expr = e.target.value; requestSql(); }));
    root.querySelectorAll('.m-name').forEach(el => el.addEventListener('input', e => { state.mapping[+e.target.dataset.i].target = e.target.value; requestSql(); }));
    root.querySelectorAll('.m-del').forEach(el => el.addEventListener('click', e => { state.mapping.splice(+e.target.dataset.i, 1); renderMapper(); requestSql(); }));
  }

  function addCustomColumn() {
    let n = 1, name = '_custom';
    const taken = {};
    state.mapping.forEach(m => { taken[m.target] = true; });
    while (taken[name]) { n++; name = '_custom' + n; }
    state.mapping.push({ target: name, group: 'Custom', expectedType: 'string', defaultCast: '', custom: true, kind: 'source', source: '', cast: '', constant: '', expr: '' });
    renderMapper();
    requestSql();
  }

  // Build the final SELECT columns from the mapping (source mappings, constants, expressions).
  function mappedColumns() {
    const out = [];
    state.mapping.forEach(m => {
      const target = (m.target || '').trim();
      if (!target || m.kind === 'none') { return; }
      if (m.kind === 'source') {
        if (!m.source) { return; }
        const sc = state.sourceColumns.find(c => c.name === m.source) || {};
        out.push({ name: m.source, alias: target, type: sc.type || '', targetType: m.cast || '' });
      } else if (m.kind === 'const') {
        if (m.constant.trim() === '') { return; }
        out.push({ alias: target, expr: formatConstant(m.constant, m) });
      } else if (m.kind === 'expr') {
        if (m.expr.trim() === '') { return; }
        out.push({ alias: target, expr: m.expr.trim() });
      }
    });
    return out;
  }

  // A constant becomes a numeric literal for number columns (or numeric-looking
  // custom values); everything else is single-quoted as a string.
  function formatConstant(v, m) {
    const s = String(v).trim();
    const numeric = s !== '' && !isNaN(Number(s));
    if (m.expectedType === 'number') { return s; }
    if (m.custom && numeric) { return s; }
    return "'" + s.replace(/'/g, "''") + "'";
  }

  function requestSql() {
    updateWarnings();
    // Only meaningful when creating a DataSet.
    if (!$('create-dataset').checked) { return; }
    if (sqlTimer) { clearTimeout(sqlTimer); }
    sqlTimer = setTimeout(() => {
      const cols = mappedColumns();
      if (!cols.length) { $('sql').textContent = ''; return; }
      vscode.postMessage({ type: 'generateSql', source: state.source, dataSourceName: $('ds-name').value.trim() || state.dataSourceName, columns: cols, castMode: 'ambiguous' });
    }, 200);
  }

  // Run the generated SQL against the draft source and show the result.
  function requestPreviewDataset() {
    if (!$('sql').textContent.trim()) { showError('Map at least one column before previewing.'); return; }
    clearError();
    readSourceOptions();
    $('dataset-preview-note').textContent = 'running…';
    vscode.postMessage({ type: 'previewDataset', source: state.source, dataSourceName: $('ds-name').value.trim() || state.dataSourceName, sql: $('sql').textContent });
  }

  function renderDatasetPreview(m) {
    $('dataset-preview-card').classList.remove('hidden');
    if (m.error) {
      $('dataset-preview-note').textContent = '';
      $('dataset-preview').innerHTML = '<div class="error">' + esc(m.error) + '</div>';
      return;
    }
    const cols = (m.columns || []).map(c => (typeof c === 'string' ? c : c.name));
    const rows = m.rows || [];
    $('dataset-preview-note').textContent = '· ' + rows.length + ' row' + (rows.length === 1 ? '' : 's') + (m.truncated ? '+' : '');
    if (!cols.length) { $('dataset-preview').innerHTML = '<div class="muted">Query returned no columns.</div>'; return; }
    let html = '<table><thead><tr>' + cols.map(c => '<th>' + esc(c) + '</th>').join('') + '</tr></thead><tbody>';
    for (const row of rows) {
      html += '<tr>' + cols.map(c => '<td>' + esc(fmtCell(row[c])) + '</td>').join('') + '</tr>';
    }
    html += '</tbody></table>';
    $('dataset-preview').innerHTML = html;
  }

  // Show the DataSet name + column mapper + SQL editor + preview only while "Create matching DataSet" is on.
  function syncDataSet() {
    const on = $('create-dataset').checked;
    $('dataset-name-wrap').classList.toggle('hidden', !on);
    $('dataset-section').classList.toggle('hidden', !on);
    if (on) { requestSql(); }
  }

  function renderFooter() {
    $('ds-name').value = state.dataSourceName;
    $('dataset-name').value = state.dataSetName;
    $('ds-name').addEventListener('input', requestSql);
    $('create-dataset').addEventListener('change', syncDataSet);
    $('preview-dataset').addEventListener('click', requestPreviewDataset);
    $('add-custom-col').addEventListener('click', addCustomColumn);
    $('create').addEventListener('click', create);
    syncDataSet();
  }

  function create() {
    readSourceOptions();
    const createDataSet = $('create-dataset').checked;
    const req = {
      source: state.source,
      dataSourceName: $('ds-name').value.trim() || state.dataSourceName,
      dataSetName: $('dataset-name').value.trim() || state.dataSetName,
      createDataSet,
      castMode: 'ambiguous',
      columns: createDataSet ? mappedColumns() : [],
      // SQL is generated from the columns by the CLI (read-only in the UI).
      sql: '',
    };
    if (!req.dataSourceName) { showError('DataSource name is required.'); return; }
    vscode.postMessage({ type: 'create', payload: req });
  }
`;
}
