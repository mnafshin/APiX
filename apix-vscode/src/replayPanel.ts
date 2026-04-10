import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { ReplaySpec, HttpResponse, HttpTransaction } from './types';

/**
 * ReplayPanel opens a WebviewPanel that lets the user modify and re-send a
 * previously captured HTTP request.
 *
 * Flow:
 *   1. User right-clicks a traffic item and chooses "Replay"
 *   2. ReplayPanel.show() is called with the transaction ID
 *   3. Panel loads request details from the engine (via GetHistory) and
 *      renders an editable form
 *   4. User edits headers/body and clicks Send
 *   5. ReplayRequest RPC is called and the response is displayed
 */
export class ReplayPanel {
    private static readonly viewType = 'apixReplay';

    public static async show(
        extensionUri: vscode.Uri,
        client: EngineClient,
        requestId: string
    ): Promise<void> {
        const panel = vscode.window.createWebviewPanel(
            ReplayPanel.viewType,
            'Replay Request',
            vscode.ViewColumn.Active,
            { enableScripts: true }
        );

        // Fetch transaction from history
        let tx: HttpTransaction | undefined;
        try {
            const [history, _cancel] = await client.getHistory({
                limit: 500,
                offset: 0,
                urlFilter: '',
                methodFilter: '',
                statusFilter: 0,
                sinceMs: 0,
            });
            tx = history.find(t => t.id === requestId);
        } catch (err: any) {
            vscode.window.showErrorMessage(`APiX: Could not load request — ${err?.message || err}`);
        }

        panel.webview.html = ReplayPanel._getHtml(panel.webview, tx);

        panel.webview.onDidReceiveMessage(async (message: { type: string; data: any }) => {
            if (message.type === 'send') {
                try {
                    const data = message.data;
                    let overrideHeaders: Record<string, string> = {};
                    try { overrideHeaders = JSON.parse(data.overrideHeaders || '{}'); } catch { /* ignore */ }

                    const spec: ReplaySpec = {
                        overrideHeaders,
                        overrideBody: data.overrideBody || '',
                        followRedirects: data.followRedirects === true,
                    };

                    if (data.useRaw) {
                        let headers: Record<string, string> = {};
                        try { headers = JSON.parse(data.headers || '{}'); } catch { /* ignore */ }
                        spec.rawRequest = {
                            id: '',
                            method: data.method || 'GET',
                            url: data.url || '',
                            headers,
                            body: data.body || '',
                            timestamp: Date.now(),
                        };
                    } else {
                        spec.requestId = requestId;
                    }

                    const response = await ReplayPanel._sendReplay(client, spec);
                    panel.webview.postMessage({ type: 'response', data: response });
                } catch (err: any) {
                    panel.webview.postMessage({ type: 'error', message: err?.message || String(err) });
                }
            }
        });
    }

    private static async _sendReplay(
        client: EngineClient,
        spec: ReplaySpec
    ): Promise<HttpResponse> {
        return client.replayRequest(spec);
    }

    private static _getNonce(): string {
        let text = '';
        const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
        for (let i = 0; i < 32; i++) {
            text += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        return text;
    }

    private static _getHtml(webview: vscode.Webview, tx: HttpTransaction | undefined): string {
        const nonce = ReplayPanel._getNonce();
        const req = tx?.request;
        const escAttr = (s: string) => s.replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        const headersJson = JSON.stringify(req?.headers || {}, null, 2);
        const bodyStr = req?.body ? String(req.body) : '';

        return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'nonce-${nonce}'; script-src 'nonce-${nonce}';">
  <title>Replay Request</title>
  <style nonce="${nonce}">
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); margin: 0; padding: 16px; }
    h2 { margin-top: 0; font-size: 16px; }
    label { display: block; font-size: 12px; color: var(--vscode-descriptionForeground); margin-bottom: 2px; margin-top: 12px; }
    input, select, textarea { width: 100%; box-sizing: border-box; background: var(--vscode-input-background); color: var(--vscode-input-foreground); border: 1px solid var(--vscode-input-border, #555); padding: 5px 8px; border-radius: 3px; font-family: inherit; font-size: 13px; }
    input:focus, select:focus, textarea:focus { outline: 1px solid var(--vscode-focusBorder); }
    .row { display: flex; gap: 8px; }
    .row select { width: 120px; flex-shrink: 0; }
    .row input { flex: 1; }
    textarea { resize: vertical; min-height: 80px; font-family: var(--vscode-editor-font-family, monospace); }
    .btn { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 6px 16px; border-radius: 3px; cursor: pointer; font-size: 13px; margin-top: 16px; }
    .btn:hover { background: var(--vscode-button-hoverBackground); }
    .checkbox-row { display: flex; align-items: center; gap: 6px; margin-top: 12px; font-size: 13px; }
    .checkbox-row input { width: auto; }
    hr { border: none; border-top: 1px solid var(--vscode-panel-border); margin: 20px 0; }
    #response-section { display: none; }
    #response-section.visible { display: block; }
    pre { background: var(--vscode-textCodeBlock-background); padding: 8px; border-radius: 4px; overflow-x: auto; white-space: pre-wrap; word-break: break-all; font-size: 12px; margin: 4px 0; }
    .status-badge { display: inline-block; padding: 2px 8px; border-radius: 3px; font-weight: 600; font-size: 13px; }
    .status-2xx { background: var(--vscode-charts-green); color: #fff; }
    .status-4xx { background: var(--vscode-charts-orange); color: #fff; }
    .status-5xx { background: var(--vscode-charts-red); color: #fff; }
    .status-3xx { background: var(--vscode-charts-blue); color: #fff; }
    .spinner { display: none; margin-left: 8px; }
    .spinner.active { display: inline; }
    .error-msg { color: var(--vscode-charts-red); margin-top: 8px; }
  </style>
</head>
<body>
  <h2>↺ Replay Request</h2>
  <div class="row">
    <select id="method">
      <option>GET</option><option>POST</option><option>PUT</option>
      <option>DELETE</option><option>PATCH</option><option>HEAD</option><option>OPTIONS</option>
    </select>
    <input type="text" id="url" value="${escAttr(req?.url || '')}" placeholder="https://..." />
  </div>
  <label>Headers (JSON)</label>
  <textarea id="headers" rows="6">${escAttr(headersJson)}</textarea>
  <label>Body</label>
  <textarea id="body" rows="6">${escAttr(bodyStr)}</textarea>
  <hr>
  <h3 style="font-size:14px;margin:0 0 8px">Overrides (applied on top of original)</h3>
  <label>Override Headers (JSON — merged with original)</label>
  <textarea id="override-headers" rows="3">{}</textarea>
  <label>Override Body (leave empty to use original)</label>
  <textarea id="override-body" rows="3"></textarea>
  <div class="checkbox-row">
    <input type="checkbox" id="follow-redirects" checked />
    <label for="follow-redirects" style="margin:0">Follow redirects</label>
  </div>
  <div class="checkbox-row">
    <input type="checkbox" id="use-raw" />
    <label for="use-raw" style="margin:0">Use edited request above (instead of original from history)</label>
  </div>
  <br>
  <button class="btn" onclick="sendRequest()">▶ Send</button>
  <span id="spinner" class="spinner">⏳</span>
  <div id="error-msg" class="error-msg"></div>

  <div id="response-section">
    <hr>
    <h3 style="font-size:14px;margin:0 0 12px">Response</h3>
    <div><span id="status-badge" class="status-badge"></span></div>
    <label>Response Headers</label><pre id="resp-headers"></pre>
    <label>Response Body</label><pre id="resp-body"></pre>
  </div>

  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();

    // Set initial method
    const methodEl = document.getElementById('method');
    const initMethod = ${JSON.stringify(req?.method || 'GET')};
    for (let i = 0; i < methodEl.options.length; i++) {
      if (methodEl.options[i].value === initMethod) { methodEl.selectedIndex = i; break; }
    }

    window.addEventListener('message', function(event) {
      const msg = event.data;
      document.getElementById('spinner').classList.remove('active');
      if (msg.type === 'response') {
        showResponse(msg.data);
      } else if (msg.type === 'error') {
        document.getElementById('error-msg').textContent = 'Error: ' + msg.message;
      }
    });

    function sendRequest() {
      document.getElementById('error-msg').textContent = '';
      document.getElementById('spinner').classList.add('active');
      vscode.postMessage({ type: 'send', data: {
        method: document.getElementById('method').value,
        url: document.getElementById('url').value,
        headers: document.getElementById('headers').value,
        body: document.getElementById('body').value,
        overrideHeaders: document.getElementById('override-headers').value,
        overrideBody: document.getElementById('override-body').value,
        followRedirects: document.getElementById('follow-redirects').checked,
        useRaw: document.getElementById('use-raw').checked,
      }});
    }

    function showResponse(resp) {
      const section = document.getElementById('response-section');
      section.classList.add('visible');
      const status = resp.statusCode || 0;
      const badge = document.getElementById('status-badge');
      badge.textContent = status + ' ' + (resp.statusText || '');
      badge.className = 'status-badge ' +
        (status >= 500 ? 'status-5xx' : status >= 400 ? 'status-4xx' : status >= 300 ? 'status-3xx' : 'status-2xx');
      document.getElementById('resp-headers').textContent = JSON.stringify(resp.headers || {}, null, 2);
      document.getElementById('resp-body').textContent = resp.body ? String(resp.body) : '(empty)';
      section.scrollIntoView({ behavior: 'smooth' });
    }
  </script>
</body>
</html>`;
    }
}

