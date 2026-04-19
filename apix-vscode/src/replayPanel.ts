import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { ReplaySpec, HttpResponse, HttpTransaction, RequestTemplate } from './types';

export class ReplayPanel {
    private static readonly viewType = 'apixReplay';

    public static async show(
        extensionUri: vscode.Uri,
        client: EngineClient,
        requestId?: string
    ): Promise<void> {
        const panel = vscode.window.createWebviewPanel(
            ReplayPanel.viewType,
            requestId ? 'Replay Request' : 'Request Composer',
            vscode.ViewColumn.Active,
            { enableScripts: true }
        );

        let tx: HttpTransaction | undefined;
        if (requestId) {
            try {
                const [history] = await client.getHistory({
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
        }

        const templates = await ReplayPanel.safeListTemplates(client);
        panel.webview.html = ReplayPanel._getHtml(panel.webview, tx, requestId, templates);

        const postTemplates = async () => {
            panel.webview.postMessage({
                type: 'templates',
                data: await ReplayPanel.safeListTemplates(client),
            });
        };

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

                    let headers: Record<string, string> = {};
                    try { headers = JSON.parse(data.headers || '{}'); } catch { /* ignore */ }

                    const rawRequest = {
                        id: '',
                        method: data.method || 'GET',
                        url: data.url || '',
                        headers,
                        body: data.body || '',
                        timestamp: Date.now(),
                    };

                    let response: HttpResponse;
                    if (!requestId || data.useRaw) {
                        response = await client.composeRequest(rawRequest, data.followRedirects === true);
                    } else {
                        spec.requestId = requestId;
                        response = await ReplayPanel._sendReplay(client, spec);
                    }
                    panel.webview.postMessage({ type: 'response', data: response });
                } catch (err: any) {
                    panel.webview.postMessage({ type: 'error', message: err?.message || String(err) });
                }
            }

            if (message.type === 'saveTemplate') {
                try {
                    const data = message.data;
                    let headers: Record<string, string> = {};
                    try { headers = JSON.parse(data.headers || '{}'); } catch { /* ignore */ }
                    await client.saveRequestTemplate({
                        id: data.id || '',
                        name: data.name || '',
                        request: {
                            id: '',
                            method: data.method || 'GET',
                            url: data.url || '',
                            headers,
                            body: data.body || '',
                            timestamp: Date.now(),
                        },
                    });
                    await postTemplates();
                } catch (err: any) {
                    panel.webview.postMessage({ type: 'error', message: err?.message || String(err) });
                }
            }

            if (message.type === 'deleteTemplate') {
                try {
                    const id = String(message.data?.id || '');
                    if (!id) {
                        throw new Error('Template id is required');
                    }
                    await client.deleteRequestTemplate(id);
                    await postTemplates();
                } catch (err: any) {
                    panel.webview.postMessage({ type: 'error', message: err?.message || String(err) });
                }
            }
        });
    }

    private static async safeListTemplates(client: EngineClient): Promise<RequestTemplate[]> {
        try {
            return await client.listRequestTemplates();
        } catch {
            return [];
        }
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

    private static _getHtml(webview: vscode.Webview, tx: HttpTransaction | undefined, requestId: string | undefined, templates: RequestTemplate[]): string {
        const nonce = ReplayPanel._getNonce();
        const csp = webview.cspSource;
        const req = tx?.request;
        const escAttr = (s: string) => s.replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        const headersJson = JSON.stringify(req?.headers || {}, null, 2);
        const bodyStr = req?.body ? String(req.body) : '';

        return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-${nonce}'; style-src 'nonce-${nonce}' ${csp}; connect-src ${csp}; img-src ${csp} data:; font-src ${csp} data:; object-src 'none'; frame-ancestors 'none'; base-uri 'none';">
  <title>${requestId ? 'Replay Request' : 'Request Composer'}</title>
  <style nonce="${nonce}">
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); margin: 0; padding: 16px; }
    h2 { margin-top: 0; font-size: 16px; }
    label { display: block; font-size: 12px; color: var(--vscode-descriptionForeground); margin-bottom: 2px; margin-top: 12px; }
    input, select, textarea { width: 100%; box-sizing: border-box; background: var(--vscode-input-background); color: var(--vscode-input-foreground); border: 1px solid var(--vscode-input-border, #555); padding: 5px 8px; border-radius: 3px; font-family: inherit; font-size: 13px; }
    .row { display: flex; gap: 8px; }
    .row > * { flex: 1; }
    .row .small { max-width: 140px; }
    textarea { resize: vertical; min-height: 80px; font-family: var(--vscode-editor-font-family, monospace); }
    .btn { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 6px 12px; border-radius: 3px; cursor: pointer; font-size: 13px; margin-top: 10px; margin-right: 8px; }
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
  <h2>${requestId ? '↺ Replay Request' : '✎ Request Composer'}</h2>
  <div class="row">
    <input id="template-name" placeholder="Template name" />
    <input id="template-id" placeholder="Template id (optional)" />
  </div>
  <div class="row">
    <select id="template-select"></select>
    <button class="btn" onclick="loadTemplate()">Load</button>
    <button class="btn" onclick="saveTemplate()">Save</button>
    <button class="btn" onclick="deleteTemplate()">Delete</button>
  </div>
  <div class="row">
    <select id="method" class="small">
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
  <h3 style="font-size:14px;margin:0 0 8px">Overrides (for replay mode)</h3>
  <label>Override Headers (JSON — merged with original)</label>
  <textarea id="override-headers" rows="3">{}</textarea>
  <label>Override Body (leave empty to use original)</label>
  <textarea id="override-body" rows="3"></textarea>
  <div class="checkbox-row">
    <input type="checkbox" id="follow-redirects" checked />
    <label for="follow-redirects" style="margin:0">Follow redirects</label>
  </div>
  <div class="checkbox-row" ${requestId ? '' : 'style="display:none"'}>
    <input type="checkbox" id="use-raw" ${requestId ? '' : 'checked'} />
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
    let templates = ${JSON.stringify(templates)};

    const methodEl = document.getElementById('method');
    const initMethod = ${JSON.stringify(req?.method || 'GET')};
    for (let i = 0; i < methodEl.options.length; i++) {
      if (methodEl.options[i].value === initMethod) { methodEl.selectedIndex = i; break; }
    }

    function renderTemplates() {
      const sel = document.getElementById('template-select');
      sel.innerHTML = '<option value="">Saved templates...</option>';
      templates.forEach(t => {
        const o = document.createElement('option');
        o.value = t.id;
        o.textContent = (t.name || '(unnamed)') + ' [' + t.id + ']';
        sel.appendChild(o);
      });
    }

    function loadTemplate() {
      const id = document.getElementById('template-select').value;
      const tpl = templates.find(t => t.id === id);
      if (!tpl || !tpl.request) { return; }
      document.getElementById('template-id').value = tpl.id || '';
      document.getElementById('template-name').value = tpl.name || '';
      document.getElementById('method').value = tpl.request.method || 'GET';
      document.getElementById('url').value = tpl.request.url || '';
      document.getElementById('headers').value = JSON.stringify(tpl.request.headers || {}, null, 2);
      document.getElementById('body').value = tpl.request.body ? String(tpl.request.body) : '';
    }

    function saveTemplate() {
      vscode.postMessage({ type: 'saveTemplate', data: {
        id: document.getElementById('template-id').value,
        name: document.getElementById('template-name').value,
        method: document.getElementById('method').value,
        url: document.getElementById('url').value,
        headers: document.getElementById('headers').value,
        body: document.getElementById('body').value,
      }});
    }

    function deleteTemplate() {
      const id = document.getElementById('template-id').value || document.getElementById('template-select').value;
      if (!id) {
        document.getElementById('error-msg').textContent = 'Error: Template id is required to delete';
        return;
      }
      vscode.postMessage({ type: 'deleteTemplate', data: { id } });
      document.getElementById('template-id').value = '';
      document.getElementById('template-name').value = '';
    }

    window.addEventListener('message', function(event) {
      const msg = event.data;
      document.getElementById('spinner').classList.remove('active');
      if (msg.type === 'response') {
        showResponse(msg.data);
      } else if (msg.type === 'error') {
        document.getElementById('error-msg').textContent = 'Error: ' + msg.message;
      } else if (msg.type === 'templates') {
        templates = msg.data || [];
        renderTemplates();
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
        useRaw: document.getElementById('use-raw') ? document.getElementById('use-raw').checked : true,
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

    renderTemplates();
  </script>
</body>
</html>`;
    }
}
