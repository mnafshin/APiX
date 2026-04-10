import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { HttpTransaction } from './types';

/**
 * TrafficPanel renders live HTTP traffic in a VS Code WebviewPanel.
 * It subscribes to the CaptureTraffic gRPC stream and pushes updates to the
 * webview via postMessage.
 */
export class TrafficPanel {
    public static currentPanel: TrafficPanel | undefined;
    private static readonly viewType = 'apixTraffic';

    private readonly _panel: vscode.WebviewPanel;
    private readonly _extensionUri: vscode.Uri;
    private _disposables: vscode.Disposable[] = [];

    public static createOrShow(extensionUri: vscode.Uri, client: EngineClient): void {
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
                localResourceRoots: [vscode.Uri.joinPath(extensionUri, 'media')],
            }
        );

        TrafficPanel.currentPanel = new TrafficPanel(panel, extensionUri, client);
    }

    private constructor(
        panel: vscode.WebviewPanel,
        extensionUri: vscode.Uri,
        private readonly client: EngineClient
    ) {
        this._panel = panel;
        this._extensionUri = extensionUri;

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

    /** Start the CaptureTraffic gRPC stream and push each transaction to the webview. */
    private _startCapture(): void {
        try {
            const stream = this.client.captureTraffic(
                (tx) => {
                    if (this._panel.visible) {
                        this._panel.webview.postMessage({ type: 'transaction', data: tx });
                    }
                },
                (err) => console.error('[APiX] CaptureTraffic error:', err)
            );
            this._disposables.push({ dispose: () => stream.cancel() });
        } catch (err) {
            console.error('[APiX] Failed to start capture stream:', err);
        }
    }

    private _handleWebviewMessage(message: { type: string; data: any }): void {
        switch (message.type) {
            case 'replay':
                if (message.data?.requestId) {
                    vscode.commands.executeCommand('apix.replayRequest', message.data.requestId);
                }
                break;
            case 'addBreakpoint':
                vscode.commands.executeCommand('apix.addBreakpoint');
                break;
            case 'inspectRequest':
                // Detail is handled client-side; nothing to do here
                break;
        }
    }

    private _update(): void {
        this._panel.webview.html = this._getHtml();
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
    th { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--vscode-panel-border); font-weight: 600; position: sticky; top: 0; background: var(--vscode-editor-background); z-index: 1; }
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
    .toolbar { display: flex; gap: 8px; margin-bottom: 8px; align-items: center; }
    .toolbar input { flex: 1; background: var(--vscode-input-background); color: var(--vscode-input-foreground); border: 1px solid var(--vscode-input-border, #555); padding: 4px 8px; border-radius: 3px; font-family: inherit; font-size: 13px; }
    .toolbar input:focus { outline: 1px solid var(--vscode-focusBorder); }
    .toolbar button { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 4px 12px; border-radius: 3px; cursor: pointer; font-family: inherit; font-size: 13px; }
    .toolbar button:hover { background: var(--vscode-button-hoverBackground); }
    .empty { text-align: center; padding: 40px; color: var(--vscode-descriptionForeground); }
    #detail { display: none; position: fixed; top: 0; right: 0; width: 50%; height: 100%; background: var(--vscode-editor-background); border-left: 1px solid var(--vscode-panel-border); padding: 16px; overflow-y: auto; z-index: 10; box-sizing: border-box; }
    #detail.open { display: block; }
    #detail h3 { margin-top: 0; font-size: 14px; word-break: break-all; }
    #detail h4 { font-size: 12px; margin: 12px 0 4px; color: var(--vscode-descriptionForeground); }
    #detail pre { background: var(--vscode-textCodeBlock-background); padding: 8px; border-radius: 4px; overflow-x: auto; white-space: pre-wrap; word-break: break-all; font-size: 12px; margin: 0; }
    #detail .detail-actions { display: flex; gap: 8px; margin-top: 12px; }
    #detail .detail-actions button { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 4px 12px; border-radius: 3px; cursor: pointer; font-size: 13px; }
    #detail .close-btn { float: right; background: transparent; color: var(--vscode-foreground); border: none; cursor: pointer; font-size: 16px; padding: 0; }
  </style>
</head>
<body>
  <div class="toolbar">
    <input type="text" id="filter" placeholder="Filter by URL, method, or status..." />
    <button onclick="clearAll()">Clear</button>
  </div>
  <table>
    <thead>
      <tr><th>#</th><th>Method</th><th>URL</th><th>Status</th><th>Duration</th><th>Time</th></tr>
    </thead>
    <tbody id="traffic"></tbody>
  </table>
  <div id="empty" class="empty">No traffic captured yet. Send requests through the proxy to see them here.</div>
  <div id="detail">
    <button class="close-btn" onclick="closeDetail()">✕</button>
    <h3 id="detail-title"></h3>
    <h4>Request Headers</h4><pre id="detail-req-headers"></pre>
    <h4>Request Body</h4><pre id="detail-req-body"></pre>
    <h4>Response Headers</h4><pre id="detail-resp-headers"></pre>
    <h4>Response Body</h4><pre id="detail-resp-body"></pre>
    <div class="detail-actions">
      <button onclick="replayRequest()">↺ Replay</button>
      <button onclick="addBreakpoint()">⊕ Add Breakpoint</button>
    </div>
  </div>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    let transactions = [];
    let count = 0;

    window.addEventListener('message', function(event) {
      const msg = event.data;
      if (msg.type === 'transaction') {
        count++;
        transactions.push(msg.data);
        addRow(msg.data, count);
        document.getElementById('empty').style.display = 'none';
      }
    });

    function addRow(tx, num) {
      const tbody = document.getElementById('traffic');
      const method = (tx.request && tx.request.method) ? tx.request.method : 'GET';
      const url = (tx.request && tx.request.url) ? tx.request.url : '';
      const status = (tx.response && tx.response.statusCode) ? tx.response.statusCode : '-';
      const methodClass = 'method-' + method.toLowerCase();
      const statusClass = (typeof status === 'number')
        ? (status >= 500 ? 'status-5xx' : status >= 400 ? 'status-4xx' : status >= 300 ? 'status-3xx' : 'status-2xx')
        : '';
      const time = tx.timestamp ? new Date(tx.timestamp).toLocaleTimeString() : '';
      const duration = tx.durationMs ? tx.durationMs + 'ms' : '-';
      const tr = document.createElement('tr');
      tr.innerHTML =
        '<td>' + num + '</td>' +
        '<td class="method ' + methodClass + '">' + escHtml(method) + '</td>' +
        '<td title="' + escHtml(url) + '">' + escHtml(url) + '</td>' +
        '<td class="' + statusClass + '">' + escHtml(String(status)) + '</td>' +
        '<td>' + escHtml(duration) + '</td>' +
        '<td>' + escHtml(time) + '</td>';
      tr.onclick = function() { showDetail(tx); };
      tbody.prepend(tr);
      applyFilter();
    }

    function escHtml(str) {
      return String(str).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
    }

    function showDetail(tx) {
      document.getElementById('detail').classList.add('open');
      const method = (tx.request && tx.request.method) || '';
      const url = (tx.request && tx.request.url) || '';
      document.getElementById('detail-title').textContent = method + ' ' + url;
      document.getElementById('detail-req-headers').textContent =
        JSON.stringify((tx.request && tx.request.headers) || {}, null, 2);
      document.getElementById('detail-req-body').textContent =
        (tx.request && tx.request.body) ? String(tx.request.body) : '(empty)';
      document.getElementById('detail-resp-headers').textContent =
        JSON.stringify((tx.response && tx.response.headers) || {}, null, 2);
      document.getElementById('detail-resp-body').textContent =
        (tx.response && tx.response.body) ? String(tx.response.body) : '(empty)';
      window._currentTx = tx;
    }

    function closeDetail() {
      document.getElementById('detail').classList.remove('open');
    }

    function replayRequest() {
      if (window._currentTx) {
        vscode.postMessage({ type: 'replay', data: { requestId: window._currentTx.id } });
      }
    }

    function addBreakpoint() {
      vscode.postMessage({ type: 'addBreakpoint', data: {} });
    }

    function clearAll() {
      document.getElementById('traffic').innerHTML = '';
      transactions = [];
      count = 0;
      document.getElementById('empty').style.display = 'block';
      closeDetail();
    }

    function applyFilter() {
      const q = document.getElementById('filter').value.toLowerCase();
      if (!q) {
        document.querySelectorAll('#traffic tr').forEach(function(tr) { tr.style.display = ''; });
        return;
      }
      document.querySelectorAll('#traffic tr').forEach(function(tr) {
        tr.style.display = tr.textContent.toLowerCase().includes(q) ? '' : 'none';
      });
    }

    document.getElementById('filter').addEventListener('input', applyFilter);
  </script>
</body>
</html>`;
    }

    public dispose(): void {
        TrafficPanel.currentPanel = undefined;
        this._panel.dispose();
        while (this._disposables.length) {
            const d = this._disposables.pop();
            if (d) { d.dispose(); }
        }
    }
}
