import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { HttpTransaction } from './types';

interface TrafficFilterState {
    url: string;
    method: string;
    status: string;
    contentType: string;
    durationMin: string;
    durationMax: string;
    body: string;
}

/**
 * TrafficPanel renders live HTTP traffic in a VS Code WebviewPanel.
 * It subscribes to the CaptureTraffic gRPC stream and pushes updates to the
 * webview via postMessage. Filters are persisted using workspace state.
 */
export class TrafficPanel {
    public static currentPanel: TrafficPanel | undefined;
    private static readonly viewType = 'apixTraffic';
    private static readonly FILTER_STATE_KEY = 'apix.trafficFilters';

    private readonly _panel: vscode.WebviewPanel;
    private readonly _extensionUri: vscode.Uri;
    private readonly _context: vscode.ExtensionContext;
    private _disposables: vscode.Disposable[] = [];
    private _captureStream: { cancel: () => void } | undefined;
    private _captureRetryTimer: ReturnType<typeof setTimeout> | undefined;
    private _captureRetryDelayMs = 1000;
    private _filterState: TrafficFilterState = {
        url: '',
        method: '',
        status: '',
        contentType: '',
        durationMin: '',
        durationMax: '',
        body: '',
    };

    public static createOrShow(context: vscode.ExtensionContext, client: EngineClient): void {
        const column = vscode.window.activeTextEditor
            ? vscode.window.activeTextEditor.viewColumn
            : undefined;

        if (TrafficPanel.currentPanel) {
            TrafficPanel.currentPanel._panel.reveal(column);
            return;
        }

        const panel = vscode.window.createWebviewPanel(
            TrafficPanel.viewType,
            'APiX Traffic',
            column || vscode.ViewColumn.One,
            {
                enableScripts: true,
                localResourceRoots: [vscode.Uri.joinPath(context.extensionUri, 'media')],
            }
        );

        TrafficPanel.currentPanel = new TrafficPanel(panel, context, client);
    }

    private constructor(
        panel: vscode.WebviewPanel,
        context: vscode.ExtensionContext,
        private readonly client: EngineClient
    ) {
        this._panel = panel;
        this._extensionUri = context.extensionUri;
        this._context = context;

        // Restore filters from workspace state
        this._restoreFilters();

        this._update();
        this._startCapture();

        this._panel.onDidDispose(() => this.dispose(), null, this._disposables);
        this._panel.onDidChangeViewState(
            () => { if (this._panel.visible) { this._update(); } },
            null,
            this._disposables
        );

        // Handle messages from the webview (e.g., user clicks Replay)
        this._panel.webview.onDidReceiveMessage(
            (message) => this._handleWebviewMessage(message),
            null,
            this._disposables
        );
    }

    private _restoreFilters(): void {
        try {
            const stored = this._context.workspaceState.get<TrafficFilterState>(TrafficPanel.FILTER_STATE_KEY);
            if (stored) {
                this._filterState = { ...this._filterState, ...stored };
            }
        } catch (err) {
            // Silently fail if there's an issue restoring filters
        }
    }

    private async _saveFilters(): Promise<void> {
        try {
            await this._context.workspaceState.update(TrafficPanel.FILTER_STATE_KEY, this._filterState);
        } catch (err) {
            // Silently fail if there's an issue saving filters
        }
    }

        /** Start the CaptureTraffic gRPC stream and push each transaction to the webview. */
    private _startCapture(): void {
        if (this._captureRetryTimer) {
            clearTimeout(this._captureRetryTimer);
            this._captureRetryTimer = undefined;
        }
        this._captureStream?.cancel();
        this._captureStream = undefined;
        try {
            const stream = this.client.captureTraffic(
                (tx) => {
                    this._captureRetryDelayMs = 1000;
                    if (this._panel.visible) {
                        this._panel.webview.postMessage({ type: 'transaction', data: tx });
                        this._panel.webview.postMessage({ type: 'streamRecovered' });
                    }
                },
                (err) => {
                    const message = err?.message || String(err);
                    this._panel.webview.postMessage({ type: 'streamError', data: message });
                    this._scheduleCaptureRetry();
                },
                () => {
                    this._panel.webview.postMessage({ type: 'streamError', data: 'Capture stream ended.' });
                    this._scheduleCaptureRetry();
                }
            );
            this._captureStream = { cancel: () => stream.cancel() };
        } catch (err) {
            this._panel.webview.postMessage({ type: 'streamError', data: `Failed to start capture stream: ${err}` });
            this._scheduleCaptureRetry();
        }
    }

    private _scheduleCaptureRetry(): void {
        if (this._captureRetryTimer) {
            return;
        }
        const delay = this._captureRetryDelayMs;
        this._captureRetryDelayMs = Math.min(this._captureRetryDelayMs * 2, 30000);
        this._captureRetryTimer = setTimeout(() => {
            this._captureRetryTimer = undefined;
            if (!this._panel.visible) {
                this._scheduleCaptureRetry();
                return;
            }
            this._startCapture();
        }, delay);
    }

    private _handleWebviewMessage(message: { type: string; data: any }): void {
        switch (message.type) {
            case 'replay':
                if (message.data?.requestId) {
                    vscode.commands.executeCommand('apix.replayRequest', message.data.requestId);
                }
                break;
            case 'loadWebSocketFrames':
                if (message.data?.requestId) {
                    void this.client.getWebSocketFrames(message.data.requestId)
                        .then((frames) => this._panel.webview.postMessage({
                            type: 'websocketFrames',
                            data: { requestId: message.data.requestId, frames },
                        }))
                        .catch((err) => {
                            void vscode.window.showErrorMessage(`APiX: Failed to load WebSocket frames — ${err?.message || err}`);
                        });
                }
                break;
            case 'copyAsCurl':
                if (message.data?.transaction) {
                    vscode.commands.executeCommand('apix.copyAsCurl', message.data.transaction);
                }
                break;
            case 'clearHistory':
                vscode.commands.executeCommand('apix.clearHistory');
                break;
            case 'copyRequestId':
                if (message.data?.requestId) {
                    vscode.commands.executeCommand('apix.copyRequestId', message.data.requestId);
                }
                break;
            case 'addBreakpoint':
                vscode.commands.executeCommand('apix.addBreakpoint');
                break;
            case 'inspectRequest':
                // Detail is handled client-side; nothing to do here
                break;
            case 'saveFilters':
                if (message.data) {
                    this._filterState = { ...message.data };
                    void this._saveFilters();
                }
                break;
            case 'clearFilters':
                this._filterState = {
                    url: '',
                    method: '',
                    status: '',
                    contentType: '',
                    durationMin: '',
                    durationMax: '',
                    body: '',
                };
                void this._saveFilters();
                this._panel.webview.postMessage({ type: 'filtersCleared' });
                break;
        }
    }

    private _update(): void {
        this._panel.webview.html = this._getHtml();
        // Send initial filter state to webview after HTML is loaded
        setTimeout(() => {
            this._panel.webview.postMessage({
                type: 'initFilters',
                data: this._filterState,
            });
        }, 100);
    }

    private _getNonce(): string {
        let text = '';
        const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
        for (let i = 0; i < 32; i++) {
            text += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        return text;
    }

    private _getHtml(): string {
        const nonce = this._getNonce();
        const csp = this._panel.webview.cspSource;
        return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-${nonce}'; style-src 'nonce-${nonce}' ${csp}; connect-src ${csp}; img-src ${csp} data:; font-src ${csp} data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none';">
  <title>APiX Traffic</title>
  <style nonce="${nonce}">
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); margin: 0; padding: 8px; }
    table { width: 100%; border-collapse: collapse; font-size: 13px; }
    th { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--vscode-panel-border); font-weight: 600; position: sticky; top: 0; background: var(--vscode-editor-background); z-index: 1; user-select: none; }
    th.sortable { cursor: pointer; }
    th.sortable:hover { background: var(--vscode-list-hoverBackground); }
    th .sort-indicator { margin-left: 4px; opacity: 0; font-size: 11px; }
    th.sort-asc .sort-indicator, th.sort-desc .sort-indicator { opacity: 1; }
    td { padding: 4px 8px; border-bottom: 1px solid var(--vscode-panel-border); cursor: pointer; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 300px; }
    tr:hover { background: var(--vscode-list-hoverBackground); }
    .method { font-weight: 600; }
    .method-get { color: var(--vscode-charts-green); }
    .method-post { color: var(--vscode-charts-blue); }
    .method-put { color: var(--vscode-charts-orange); }
    .method-delete { color: var(--vscode-charts-red); }
    .method-patch { color: var(--vscode-charts-purple); }
    .status-2xx { color: var(--vscode-charts-green); }
    .status-3xx { color: var(--vscode-charts-blue); }
    .status-4xx { color: var(--vscode-charts-orange); }
    .status-5xx { color: var(--vscode-charts-red); }
    .panel-toolbar { display: flex; justify-content: flex-end; margin-bottom: 8px; }
    .panel-toolbar button { background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); border: none; padding: 4px 12px; border-radius: 3px; cursor: pointer; font-family: inherit; font-size: 13px; }
    .panel-toolbar button:hover { background: var(--vscode-button-secondaryHoverBackground, var(--vscode-button-hoverBackground)); }
    .filter-bar { margin-bottom: 8px; display: flex; flex-direction: column; gap: 6px; }
    .filter-bar.hidden { display: none; }
    .filter-row { display: flex; gap: 6px; align-items: center; }
    .filter-row input, .filter-row select { background: var(--vscode-input-background); color: var(--vscode-input-foreground); border: 1px solid var(--vscode-input-border, #555); padding: 4px 8px; border-radius: 3px; font-family: inherit; font-size: 13px; }
    .filter-row input:focus, .filter-row select:focus { outline: 1px solid var(--vscode-focusBorder); }
    .filter-url, .filter-body, .filter-ct { flex: 1; min-width: 60px; }
    .filter-status { width: 120px; }
    .filter-dur { width: 70px; }
    .filter-sep { color: var(--vscode-descriptionForeground); font-size: 13px; }
    .filter-row button { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 4px 12px; border-radius: 3px; cursor: pointer; font-family: inherit; font-size: 13px; }
    .filter-row button:hover { background: var(--vscode-button-hoverBackground); }
    .badge { display: inline-block; padding: 1px 6px; border-radius: 999px; font-size: 11px; font-weight: 700; margin-right: 6px; }
    .badge-ws { background: var(--vscode-badge-background); color: var(--vscode-badge-foreground); }
    .empty { text-align: center; padding: 40px; color: var(--vscode-descriptionForeground); }
    #stream-error { display: none; margin-bottom: 8px; padding: 6px 8px; border-radius: 4px; border: 1px solid var(--vscode-inputValidation-errorBorder, #be1100); background: var(--vscode-inputValidation-errorBackground, #5a1d1d); color: var(--vscode-inputValidation-errorForeground, #fff); font-size: 12px; }
    #detail { display: none; position: fixed; top: 0; right: 0; width: 50%; height: 100%; background: var(--vscode-editor-background); border-left: 1px solid var(--vscode-panel-border); padding: 16px; overflow-y: auto; z-index: 10; box-sizing: border-box; }
    #detail.open { display: block; }
    #detail h3 { margin-top: 0; font-size: 14px; word-break: break-all; }
    #detail h4 { font-size: 12px; margin: 12px 0 4px; color: var(--vscode-descriptionForeground); }
    #detail pre { background: var(--vscode-textCodeBlock-background); padding: 8px; border-radius: 4px; overflow-x: auto; white-space: pre-wrap; word-break: break-all; font-size: 12px; margin: 0; }
    #detail .detail-actions { display: flex; gap: 8px; margin-top: 12px; }
    #detail .detail-actions button { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 4px 12px; border-radius: 3px; cursor: pointer; font-size: 13px; }
    #detail .close-btn { float: right; background: transparent; color: var(--vscode-foreground); border: none; cursor: pointer; font-size: 16px; padding: 0; }
    #detail-frames { display: none; }
    #detail-frames.open { display: block; }
    #ws-frame-list { display: grid; gap: 8px; }
    .ws-frame { background: var(--vscode-textCodeBlock-background); padding: 8px; border-radius: 4px; }
    .ws-frame-meta { display: flex; gap: 8px; font-size: 12px; color: var(--vscode-descriptionForeground); margin-bottom: 4px; }
  </style>
</head>
<body>
  <div class="panel-toolbar">
    <button id="toggle-filters-btn" onclick="toggleFilterBar()" title="Toggle filter bar (Ctrl+F)">Hide Filters</button>
  </div>
  <div id="stream-error"></div>
  <div class="filter-bar">
    <div class="filter-row">
      <input type="text" id="filter" class="filter-url" placeholder="Filter by URL..." title="Filter by URL substring" />
      <select id="filter-method" title="Filter by HTTP method">
        <option value="">All Methods</option>
        <option value="GET">GET</option>
        <option value="POST">POST</option>
        <option value="PUT">PUT</option>
        <option value="DELETE">DELETE</option>
        <option value="PATCH">PATCH</option>
      </select>
      <input type="text" id="filter-status" class="filter-status" placeholder="Status (2xx, 200)" title="Filter by status code or range (e.g. 2xx, 4xx, 404)" />
      <button onclick="clearFilters()" id="clear-btn">Clear Filters</button>
      <span id="filter-status-badge" class="filter-status-badge"></span>
    </div>
    <div class="filter-row">
      <input type="text" id="filter-ct" class="filter-ct" placeholder="Content-Type…" title="Filter by response Content-Type substring" />
      <input type="number" id="filter-dur-min" class="filter-dur" placeholder="Min ms" title="Minimum duration (ms)" />
      <span class="filter-sep">–</span>
      <input type="number" id="filter-dur-max" class="filter-dur" placeholder="Max ms" title="Maximum duration (ms)" />
      <input type="text" id="filter-body" class="filter-body" placeholder="Body search…" title="Search in request or response body" />
    </div>
  </div>
  <table>
    <thead>
      <tr><th>#</th><th class="sortable" data-sort-key="method">Method<span class="sort-indicator"></span></th><th class="sortable" data-sort-key="url">URL<span class="sort-indicator"></span></th><th class="sortable" data-sort-key="status">Status<span class="sort-indicator"></span></th><th class="sortable" data-sort-key="duration">Duration<span class="sort-indicator"></span></th><th class="sortable" data-sort-key="time">Time<span class="sort-indicator"></span></th></tr>
    </thead>
    <tbody id="traffic"></tbody>
  </table>
  <div id="empty" class="empty">No traffic captured yet. Send requests through the proxy to see them here.</div>
  <div id="detail">
    <button class="close-btn" onclick="closeDetail()">✕</button>
    <h3 id="detail-title"></h3>
    <h4>Request Headers</h4><pre id="detail-req-headers"></pre>
    <h4>Request ID</h4><pre id="detail-request-id"></pre>
    <h4>Request Body</h4><pre id="detail-req-body"></pre>
    <h4>Response Headers</h4><pre id="detail-resp-headers"></pre>
    <h4>Response Body</h4><pre id="detail-resp-body"></pre>
    <h4 id="detail-graphql-title" style="display:none;">GraphQL</h4><pre id="detail-graphql" style="display:none;"></pre>
    <div id="detail-frames">
      <h4>WebSocket Frames</h4>
      <div id="ws-frame-list"></div>
    </div>
    <div class="detail-actions">
      <button onclick="replayRequest()">↺ Replay</button>
      <button onclick="copyAsCurl()">⎘ Copy as curl</button>
      <button onclick="copyRequestId()">⎘ Copy Request ID</button>
      <button onclick="addBreakpoint()">⊕ Add Breakpoint</button>
    </div>
  </div>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    let transactions = [];
    let count = 0;
    let currentFramesRequestId = '';
    let sortKey = null;
    let sortAsc = true;
    let filtersVisible = true;

    window.addEventListener('message', function(event) {
      const msg = event.data;
      if (msg.type === 'transaction') {
        count++;
        transactions.push(msg.data);
        addRow(msg.data, count);
        document.getElementById('empty').style.display = 'none';
      } else if (msg.type === 'websocketFrames') {
        if (msg.data && msg.data.requestId === currentFramesRequestId) {
          renderWebSocketFrames(msg.data.frames || []);
        }
      } else if (msg.type === 'streamError') {
        const el = document.getElementById('stream-error');
        el.textContent = 'Connection lost — retrying… ' + (msg.data || '');
        el.style.display = 'block';
      } else if (msg.type === 'streamRecovered') {
        const el = document.getElementById('stream-error');
        el.style.display = 'none';
        el.textContent = '';
      } else if (msg.type === 'initFilters') {
        restoreFilters(msg.data || {});
      } else if (msg.type === 'filtersCleared') {
        clearAllFilters();
      } else if (msg.type === 'historyCleared') {
        clearAll();
      }
    });

    function addRow(tx, num) {
      rerenderTable();
    }

    function escHtml(str) {
      return String(str).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
    }

    function showDetail(tx) {
      document.getElementById('detail').classList.add('open');
      const method = (tx.request && tx.request.method) || '';
      const url = (tx.request && tx.request.url) || '';
      const isWebSocket = isWebSocketTransaction(tx);
      document.getElementById('detail-title').textContent = (isWebSocket ? '[WS] ' : '') + method + ' ' + url;
      document.getElementById('detail-req-headers').textContent =
        JSON.stringify((tx.request && tx.request.headers) || {}, null, 2);
      document.getElementById('detail-request-id').textContent =
        tx.requestId || tx.id || '(none)';
      document.getElementById('detail-req-body').textContent =
        (tx.request && tx.request.body) ? String(tx.request.body) : '(empty)';
      document.getElementById('detail-resp-headers').textContent =
        JSON.stringify((tx.response && tx.response.headers) || {}, null, 2);
      document.getElementById('detail-resp-body').textContent =
        (tx.response && tx.response.body) ? String(tx.response.body) : '(empty)';
      renderGraphQLDetail(tx);
      const frames = document.getElementById('detail-frames');
      if (isWebSocket) {
        currentFramesRequestId = tx.id || '';
        frames.classList.add('open');
        document.getElementById('ws-frame-list').textContent = 'Loading WebSocket frames...';
        vscode.postMessage({ type: 'loadWebSocketFrames', data: { requestId: currentFramesRequestId } });
      } else {
        currentFramesRequestId = '';
        frames.classList.remove('open');
        document.getElementById('ws-frame-list').textContent = '';
      }
      window._currentTx = tx;
    }

    function closeDetail() {
      document.getElementById('detail').classList.remove('open');
    }

    function renderGraphQLDetail(tx) {
      const titleEl = document.getElementById('detail-graphql-title');
      const bodyEl = document.getElementById('detail-graphql');
      const meta = tx.graphql || deriveGraphQLMetadata(tx);
      if (!meta || (!meta.request && !meta.response)) {
        titleEl.style.display = 'none';
        bodyEl.style.display = 'none';
        bodyEl.textContent = '';
        return;
      }

      const lines = [];
      if (meta.request) {
        lines.push('Request');
        if (meta.request.operationName) {
          lines.push('  operationName: ' + meta.request.operationName);
        }
        if (meta.request.isBatch) {
          lines.push('  batched: true (' + String(meta.request.operationCount || 0) + ' operations)');
        }
        if (meta.request.query) {
          lines.push('  query:');
          lines.push(indentBlock(meta.request.query, '    '));
        }
        if (meta.request.variablesJson) {
          lines.push('  variables:');
          lines.push(indentBlock(prettyJson(meta.request.variablesJson), '    '));
        }
      }

      const errors = (((meta.response || {}).errors) || []);
      if (errors.length > 0) {
        if (lines.length) { lines.push(''); }
        lines.push('Response Errors');
        errors.forEach(function(err, idx) {
          lines.push('  [' + String(idx + 1) + '] ' + (err.message || '(no message)'));
          if (err.pathJson) { lines.push('    path: ' + err.pathJson); }
          if (err.locationsJson) { lines.push('    locations: ' + err.locationsJson); }
          if (err.extensionsJson) { lines.push('    extensions: ' + err.extensionsJson); }
        });
      }

      titleEl.style.display = '';
      bodyEl.style.display = '';
      bodyEl.textContent = lines.join('\n');
    }

    function deriveGraphQLMetadata(tx) {
      const req = parseGraphQLRequest((tx.request && tx.request.body) || '');
      const resp = parseGraphQLResponse((tx.response && tx.response.body) || '');
      if (!req && !resp) { return null; }
      return { request: req || undefined, response: resp || undefined };
    }

    function parseGraphQLRequest(rawBody) {
      const text = safeBodyText(rawBody);
      if (!text) { return null; }
      const payload = safeJsonParse(text);
      if (!payload) { return null; }
      if (Array.isArray(payload)) {
        const ops = payload.map(parseGraphQLOperation).filter(Boolean);
        if (!ops.length) { return null; }
        const first = ops[0];
        return {
          operationName: first.operationName || '',
          query: first.query || '',
          variablesJson: first.variablesJson || '',
          isBatch: true,
          operationCount: ops.length,
        };
      }
      const op = parseGraphQLOperation(payload);
      if (!op) { return null; }
      return {
        operationName: op.operationName || '',
        query: op.query || '',
        variablesJson: op.variablesJson || '',
        isBatch: false,
        operationCount: 1,
      };
    }

    function parseGraphQLOperation(obj) {
      if (!obj || typeof obj !== 'object') { return null; }
      const hasFields = Object.prototype.hasOwnProperty.call(obj, 'query')
        || Object.prototype.hasOwnProperty.call(obj, 'operationName')
        || Object.prototype.hasOwnProperty.call(obj, 'variables');
      if (!hasFields) { return null; }
      return {
        operationName: typeof obj.operationName === 'string' ? obj.operationName : '',
        query: typeof obj.query === 'string' ? obj.query : '',
        variablesJson: obj.variables === undefined ? '' : safeJsonStringify(obj.variables),
      };
    }

    function parseGraphQLResponse(rawBody) {
      const text = safeBodyText(rawBody);
      if (!text) { return null; }
      const payload = safeJsonParse(text);
      if (!payload) { return null; }
      const errors = collectGraphQLErrors(payload);
      if (!errors.length) { return null; }
      return { errors };
    }

    function collectGraphQLErrors(payload) {
      if (Array.isArray(payload)) {
        const combined = [];
        payload.forEach(function(item) {
          const nested = collectGraphQLErrors(item);
          for (let i = 0; i < nested.length; i++) {
            combined.push(nested[i]);
          }
        });
        return combined;
      }
      if (!payload || typeof payload !== 'object' || !Array.isArray(payload.errors)) {
        return [];
      }
      return payload.errors
        .filter(function(item) { return item && typeof item === 'object'; })
        .map(function(item) {
          return {
            message: typeof item.message === 'string' ? item.message : '',
            pathJson: item.path === undefined ? '' : safeJsonStringify(item.path),
            locationsJson: item.locations === undefined ? '' : safeJsonStringify(item.locations),
            extensionsJson: item.extensions === undefined ? '' : safeJsonStringify(item.extensions),
            rawJson: safeJsonStringify(item),
          };
        });
    }

    function safeBodyText(raw) {
      if (raw === undefined || raw === null) { return ''; }
      if (typeof raw === 'string') { return raw; }
      if (raw && Array.isArray(raw.data)) {
        try {
          return new TextDecoder().decode(new Uint8Array(raw.data));
        } catch (e) {
          return '';
        }
      }
      if (Array.isArray(raw)) {
        try {
          return new TextDecoder().decode(new Uint8Array(raw));
        } catch (e) {
          return '';
        }
      }
      return String(raw);
    }

    function safeJsonParse(text) {
      try { return JSON.parse(text); } catch (e) { return null; }
    }

    function safeJsonStringify(value) {
      try { return JSON.stringify(value); } catch (e) { return ''; }
    }

    function prettyJson(text) {
      try { return JSON.stringify(JSON.parse(text), null, 2); } catch (e) { return text; }
    }

    function indentBlock(text, indent) {
      return String(text || '').split('\n').map(function(line) { return indent + line; }).join('\n');
    }

    function isWebSocketTransaction(tx) {
      const headers = (tx.request && tx.request.headers) || {};
      const upgrade = headers.Upgrade || headers.upgrade || '';
      return String(upgrade).toLowerCase() === 'websocket' || ((tx.response && tx.response.statusCode) === 101);
    }

    function opcodeLabel(opcode) {
      switch (opcode) {
        case 1: return 'text';
        case 2: return 'binary';
        case 8: return 'close';
        case 9: return 'ping';
        case 10: return 'pong';
        default: return 'opcode ' + opcode;
      }
    }

    function renderWebSocketFrames(frames) {
      const root = document.getElementById('ws-frame-list');
      if (!frames || frames.length === 0) {
        root.textContent = 'No WebSocket frames captured for this connection.';
        return;
      }
      root.innerHTML = frames.map(function(frame) {
        const payload = typeof frame.payload === 'string' ? frame.payload : String(frame.payload || '');
        const timestamp = frame.timestampMs ? new Date(frame.timestampMs).toLocaleTimeString() : '';
        const direction = frame.direction === 'client' ? '↑ client' : '↓ server';
        return '<div class="ws-frame">' +
          '<div class="ws-frame-meta">' +
          '<span>' + escHtml(direction) + '</span>' +
          '<span>' + escHtml(opcodeLabel(frame.opcode)) + '</span>' +
          '<span>' + escHtml(timestamp) + '</span>' +
          '</div>' +
          '<pre>' + escHtml(payload || '(empty)') + '</pre>' +
          '</div>';
      }).join('');
    }

    function replayRequest() {
      if (window._currentTx) {
        vscode.postMessage({ type: 'replay', data: { requestId: window._currentTx.id } });
      }
    }

    function addBreakpoint() {
      vscode.postMessage({ type: 'addBreakpoint', data: {} });
    }

    function copyAsCurl() {
      if (window._currentTx) {
        vscode.postMessage({ type: 'copyAsCurl', data: { transaction: window._currentTx } });
      }
    }

    function copyRequestId() {
      if (window._currentTx) {
        vscode.postMessage({
          type: 'copyRequestId',
          data: { requestId: window._currentTx.requestId || window._currentTx.id || '' }
        });
      }
    }

    function setFilterBarVisible(visible) {
      filtersVisible = visible;
      const filterBar = document.querySelector('.filter-bar');
      const toggleBtn = document.getElementById('toggle-filters-btn');
      if (filterBar) {
        filterBar.classList.toggle('hidden', !visible);
      }
      if (toggleBtn) {
        toggleBtn.textContent = visible ? 'Hide Filters' : 'Show Filters';
      }
      if (visible) {
        const firstFilter = document.getElementById('filter');
        if (firstFilter) {
          firstFilter.focus();
        }
      }
    }

    function toggleFilterBar() {
      setFilterBarVisible(!filtersVisible);
    }

    function clearHistory() {
      vscode.postMessage({ type: 'clearHistory', data: {} });
    }

    function clearAll() {
      document.getElementById('traffic').innerHTML = '';
      transactions = [];
      count = 0;
      currentFramesRequestId = '';
      document.getElementById('empty').style.display = 'block';
      closeDetail();
    }

    function clearFilters() {
      document.getElementById('filter').value = '';
      document.getElementById('filter-method').value = '';
      document.getElementById('filter-status').value = '';
      document.getElementById('filter-ct').value = '';
      document.getElementById('filter-dur-min').value = '';
      document.getElementById('filter-dur-max').value = '';
      document.getElementById('filter-body').value = '';
      updateFilterStatusBadge();
      vscode.postMessage({ type: 'clearFilters' });
      rerenderTable();
    }

    function clearAllFilters() {
      document.getElementById('filter').value = '';
      document.getElementById('filter-method').value = '';
      document.getElementById('filter-status').value = '';
      document.getElementById('filter-ct').value = '';
      document.getElementById('filter-dur-min').value = '';
      document.getElementById('filter-dur-max').value = '';
      document.getElementById('filter-body').value = '';
      updateFilterStatusBadge();
      rerenderTable();
    }

    function restoreFilters(filterState) {
      document.getElementById('filter').value = filterState.url || '';
      document.getElementById('filter-method').value = filterState.method || '';
      document.getElementById('filter-status').value = filterState.status || '';
      document.getElementById('filter-ct').value = filterState.contentType || '';
      document.getElementById('filter-dur-min').value = filterState.durationMin || '';
      document.getElementById('filter-dur-max').value = filterState.durationMax || '';
      document.getElementById('filter-body').value = filterState.body || '';
      updateFilterStatusBadge();
      rerenderTable();
    }

    function updateFilterStatusBadge() {
      const filters = {
        url: document.getElementById('filter').value,
        method: document.getElementById('filter-method').value,
        status: document.getElementById('filter-status').value,
        contentType: document.getElementById('filter-ct').value,
        durationMin: document.getElementById('filter-dur-min').value,
        durationMax: document.getElementById('filter-dur-max').value,
        body: document.getElementById('filter-body').value,
      };

      const activeCount = Object.values(filters).filter(v => v).length;
      const badge = document.getElementById('filter-status-badge');
      if (activeCount > 0) {
        badge.textContent = activeCount + ' filter' + (activeCount !== 1 ? 's' : '');
        badge.classList.add('active');
      } else {
        badge.textContent = '';
        badge.classList.remove('active');
      }

      // Save filters
      vscode.postMessage({ type: 'saveFilters', data: filters });
    }

    function applyFilter() {
      updateFilterStatusBadge();
      rerenderTable();
    }

    function getSortValue(tx, key) {
      switch (key) {
        case 'method':
          return (tx.request && tx.request.method) ? tx.request.method.toUpperCase() : '';
        case 'url':
          return (tx.request && tx.request.url) ? tx.request.url.toLowerCase() : '';
        case 'status':
          return (tx.response && tx.response.statusCode) ? tx.response.statusCode : 0;
        case 'duration':
          return tx.durationMs || 0;
        case 'time':
          return tx.timestamp ? new Date(tx.timestamp).getTime() : 0;
        default:
          return '';
      }
    }

    function sortTransactions(key) {
      if (sortKey === key) {
        sortAsc = !sortAsc;
      } else {
        sortKey = key;
        sortAsc = true;
      }
      updateSortIndicators();
      rerenderTable();
    }

    function updateSortIndicators() {
      document.querySelectorAll('th.sortable').forEach(function(th) {
        th.classList.remove('sort-asc', 'sort-desc');
        const indicator = th.querySelector('.sort-indicator');
        if (th.getAttribute('data-sort-key') === sortKey) {
          if (sortAsc) {
            th.classList.add('sort-asc');
            indicator.textContent = '▲';
          } else {
            th.classList.add('sort-desc');
            indicator.textContent = '▼';
          }
        } else {
          indicator.textContent = '';
        }
      });
    }

    function getVisibleTransactions() {
      const q = (document.getElementById('filter').value || '').toLowerCase();
      const method = document.getElementById('filter-method').value;
      const status = (document.getElementById('filter-status').value || '').trim().toLowerCase();
      const ctFilter = (document.getElementById('filter-ct').value || '').toLowerCase();
      const durMinVal = document.getElementById('filter-dur-min').value;
      const durMaxVal = document.getElementById('filter-dur-max').value;
      const durMin = durMinVal !== '' ? Number(durMinVal) : null;
      const durMax = durMaxVal !== '' ? Number(durMaxVal) : null;
      const bodySearch = (document.getElementById('filter-body').value || '').toLowerCase();

      const filtered = transactions.filter(function(tx, idx) {
        const txMethod = (tx.request && tx.request.method) || '';
        const txUrl = (tx.request && tx.request.url) || '';
        const txStatus = (tx.response && tx.response.statusCode) || 0;
        const txDur = tx.durationMs || 0;
        const txRespHeaders = (tx.response && tx.response.headers) || {};
        const txCt = (txRespHeaders['content-type'] || txRespHeaders['Content-Type'] || '').toLowerCase();
        const txReqBody = ((tx.request && tx.request.body) || '').toLowerCase();
        const txRespBody = ((tx.response && tx.response.body) || '').toLowerCase();

        if (q && !txUrl.toLowerCase().includes(q)) { return false; }
        if (method && txMethod !== method) { return false; }
        if (status) {
          if (/^\dxx$/.test(status)) {
            const prefix = parseInt(status[0], 10);
            if (Math.floor(txStatus / 100) !== prefix) { return false; }
          } else if (/^\d+$/.test(status)) {
            if (String(txStatus) !== status) { return false; }
          }
        }
        if (ctFilter && !txCt.includes(ctFilter)) { return false; }
        if (durMin !== null && txDur < durMin) { return false; }
        if (durMax !== null && txDur > durMax) { return false; }
        if (bodySearch && !txReqBody.includes(bodySearch) && !txRespBody.includes(bodySearch)) { return false; }
        return true;
      });

      if (sortKey) {
        filtered.sort(function(a, b) {
          const aVal = getSortValue(a, sortKey);
          const bVal = getSortValue(b, sortKey);
          let cmp = 0;
          if (aVal < bVal) { cmp = -1; }
          else if (aVal > bVal) { cmp = 1; }
          return sortAsc ? cmp : -cmp;
        });
      }

      return filtered;
    }

    function rerenderTable() {
      const tbody = document.getElementById('traffic');
      tbody.innerHTML = '';
      let displayNum = 1;
      const visible = getVisibleTransactions();
      visible.forEach(function(tx, idx) {
        const method = (tx.request && tx.request.method) ? tx.request.method : 'GET';
        const url = (tx.request && tx.request.url) ? tx.request.url : '';
        const status = (tx.response && tx.response.statusCode) ? tx.response.statusCode : '-';
        const isWebSocket = isWebSocketTransaction(tx);
        const methodClass = 'method-' + method.toLowerCase();
        const statusClass = (typeof status === 'number')
          ? (status >= 500 ? 'status-5xx' : status >= 400 ? 'status-4xx' : status >= 300 ? 'status-3xx' : 'status-2xx')
          : '';
        const time = tx.timestamp ? new Date(tx.timestamp).toLocaleTimeString() : '';
        const duration = tx.durationMs ? tx.durationMs + 'ms' : '-';
        const tr = document.createElement('tr');
        tr.innerHTML =
          '<td>' + displayNum + '</td>' +
          '<td class="method ' + methodClass + '">' + escHtml(method) + '</td>' +
          '<td title="' + escHtml(url) + '">' + (isWebSocket ? '<span class="badge badge-ws">WS</span>' : '') + escHtml(url) + '</td>' +
          '<td class="' + statusClass + '">' + escHtml(String(status)) + '</td>' +
          '<td>' + escHtml(duration) + '</td>' +
          '<td>' + escHtml(time) + '</td>';
        tr.setAttribute('data-tx-index', String(transactions.indexOf(tx)));
        tr.onclick = function() { showDetail(tx); };
        tbody.appendChild(tr);
        displayNum++;
      });
      if (visible.length === 0) {
        document.getElementById('empty').style.display = 'block';
      } else {
        document.getElementById('empty').style.display = 'none';
      }
    }

    document.getElementById('filter').addEventListener('input', applyFilter);
    document.getElementById('filter-method').addEventListener('change', applyFilter);
    document.getElementById('filter-status').addEventListener('input', applyFilter);
    document.getElementById('filter-ct').addEventListener('input', applyFilter);
    document.getElementById('filter-dur-min').addEventListener('input', applyFilter);
    document.getElementById('filter-dur-max').addEventListener('input', applyFilter);
    document.getElementById('filter-body').addEventListener('input', applyFilter);

    document.querySelectorAll('th.sortable').forEach(function(th) {
      th.addEventListener('click', function() {
        const key = this.getAttribute('data-sort-key');
        sortTransactions(key);
      });
    });

    document.addEventListener('keydown', function(event) {
      if (!(event.ctrlKey || event.metaKey) || event.altKey) {
        return;
      }

      const key = String(event.key || '').toLowerCase();
      if (key === 'f' && !event.shiftKey) {
        event.preventDefault();
        toggleFilterBar();
      } else if (key === 'r' && event.shiftKey) {
        event.preventDefault();
        replayRequest();
      } else if (key === 'c' && event.shiftKey) {
        event.preventDefault();
        copyAsCurl();
      } else if (key === 'k' && event.shiftKey) {
        event.preventDefault();
        clearHistory();
      }
    });
  </script>
</body>
</html>`;
    }

    public dispose(): void {
        TrafficPanel.currentPanel = undefined;
        this._captureStream?.cancel();
        this._captureStream = undefined;
        if (this._captureRetryTimer) {
            clearTimeout(this._captureRetryTimer);
            this._captureRetryTimer = undefined;
        }
        this._panel.dispose();
        while (this._disposables.length) {
            const d = this._disposables.pop();
            if (d) { d.dispose(); }
        }
    }

    public clearDisplayedHistory(): void {
        this._panel.webview.postMessage({ type: 'historyCleared' });
    }
}
