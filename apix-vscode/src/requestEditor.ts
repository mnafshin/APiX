import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { PausedRequest, ResumeAction, ResumeActionKind, HttpRequest, HttpResponse } from './types';

/**
 * RequestEditor opens a document-like editor for a paused request, allowing
 * the user to modify headers/body before forwarding or dropping it.
 *
 * Flow:
 *   1. PausedRequest arrives via WatchPausedRequests stream
 *   2. showEditor() opens a WebviewPanel with editable request fields
 *   3. User clicks Forward / Drop / Respond
 *   4. ResumeRequest RPC is called with the chosen action
 */
export class RequestEditor {
    private static readonly viewType = 'apixRequestEditor';

    public static async showEditor(
        extensionUri: vscode.Uri,
        client: EngineClient,
        paused: PausedRequest
    ): Promise<void> {
        const panel = vscode.window.createWebviewPanel(
            RequestEditor.viewType,
            `Paused: ${paused.request?.method || 'GET'} ${paused.request?.url || ''}`,
            vscode.ViewColumn.Active,
            { enableScripts: true }
        );

        panel.webview.html = RequestEditor._getHtml(panel.webview, paused);

        panel.webview.onDidReceiveMessage(async (message: { type: string; data: any }) => {
            try {
                let action: ResumeAction;
                switch (message.type) {
                    case 'forward': {
                        const mod = message.data as Partial<HttpRequest>;
                        const modifiedRequest: HttpRequest = {
                            id: paused.request?.id || '',
                            method: mod.method || paused.request?.method || 'GET',
                            url: mod.url || paused.request?.url || '',
                            headers: mod.headers || paused.request?.headers || {},
                            body: mod.body || paused.request?.body || '',
                            timestamp: paused.request?.timestamp || Date.now(),
                        };
                        action = RequestEditor._buildResumeAction(
                            paused.requestId, ResumeActionKind.Forward,
                            { modifiedRequest }
                        );
                        break;
                    }
                    case 'drop':
                        action = RequestEditor._buildResumeAction(
                            paused.requestId, ResumeActionKind.Drop, {}
                        );
                        break;
                    case 'respond': {
                        const resp = message.data as Partial<HttpResponse>;
                        const modifiedResponse: HttpResponse = {
                            statusCode: resp.statusCode || 200,
                            statusText: resp.statusText || 'OK',
                            headers: resp.headers || {},
                            body: resp.body || '',
                        };
                        action = RequestEditor._buildResumeAction(
                            paused.requestId, ResumeActionKind.Respond,
                            { modifiedResponse }
                        );
                        break;
                    }
                    default:
                        return;
                }
                await client.resumeRequest(action);
                panel.dispose();
            } catch (err: any) {
                vscode.window.showErrorMessage(`APiX: Failed to resume request — ${err?.message || err}`);
            }
        });
    }

    private static _buildResumeAction(
        requestId: string,
        action: ResumeActionKind,
        modifiedFields: Record<string, unknown>
    ): ResumeAction {
        return {
            requestId,
            action,
            modifiedRequest: modifiedFields.modifiedRequest as HttpRequest | undefined,
            modifiedResponse: modifiedFields.modifiedResponse as HttpResponse | undefined,
        };
    }

    private static _getNonce(): string {
        let text = '';
        const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
        for (let i = 0; i < 32; i++) {
            text += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        return text;
    }

    private static _getHtml(webview: vscode.Webview, paused: PausedRequest): string {
        const nonce = RequestEditor._getNonce();
        const req = paused.request || {};
        const headersJson = JSON.stringify(req.headers || {}, null, 2);
        const bodyStr = req.body ? String(req.body) : '';
        const escAttr = (s: string) => s.replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

        return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'nonce-${nonce}'; script-src 'nonce-${nonce}';">
  <title>Edit Request</title>
  <style nonce="${nonce}">
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); margin: 0; padding: 16px; }
    h2 { margin-top: 0; font-size: 16px; }
    h3 { font-size: 14px; margin: 0 0 8px; }
    label { display: block; font-size: 12px; color: var(--vscode-descriptionForeground); margin-bottom: 2px; margin-top: 12px; }
    input, select, textarea { width: 100%; box-sizing: border-box; background: var(--vscode-input-background); color: var(--vscode-input-foreground); border: 1px solid var(--vscode-input-border, #555); padding: 5px 8px; border-radius: 3px; font-family: inherit; font-size: 13px; }
    input:focus, select:focus, textarea:focus { outline: 1px solid var(--vscode-focusBorder); }
    .row { display: flex; gap: 8px; }
    .row select { width: 120px; flex-shrink: 0; }
    .row input { flex: 1; }
    textarea { resize: vertical; min-height: 80px; font-family: var(--vscode-editor-font-family, monospace); }
    .actions { display: flex; gap: 8px; margin-top: 20px; }
    .btn { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 6px 16px; border-radius: 3px; cursor: pointer; font-size: 13px; }
    .btn:hover { background: var(--vscode-button-hoverBackground); }
    .btn-danger { background: var(--vscode-inputValidation-errorBackground, #5a1d1d); }
    .btn-secondary { background: var(--vscode-button-secondaryBackground); color: var(--vscode-button-secondaryForeground); }
    .hidden { display: none; }
    #respond-section { display: none; border-top: 1px solid var(--vscode-panel-border); margin-top: 16px; padding-top: 16px; }
    #respond-section.visible { display: block; }
    .badge { font-size: 11px; background: var(--vscode-badge-background); color: var(--vscode-badge-foreground); padding: 2px 6px; border-radius: 3px; margin-left: 8px; }
  </style>
</head>
<body>
  <h2>⏸ Paused Request <span class="badge">Breakpoint hit</span></h2>
  <div class="row">
    <select id="method">
      <option>GET</option><option>POST</option><option>PUT</option>
      <option>DELETE</option><option>PATCH</option><option>HEAD</option><option>OPTIONS</option>
    </select>
    <input type="text" id="url" value="${escAttr(req.url || '')}" />
  </div>
  <label>Headers (JSON)</label>
  <textarea id="headers" rows="6">${escAttr(headersJson)}</textarea>
  <label>Request Body</label>
  <textarea id="body" rows="6">${escAttr(bodyStr)}</textarea>

  <div id="respond-section">
    <h3>Synthetic Response</h3>
    <label>Status Code</label>
    <input type="number" id="resp-status" value="200" />
    <label>Status Text</label>
    <input type="text" id="resp-status-text" value="OK" />
    <label>Response Headers (JSON)</label>
    <textarea id="resp-headers" rows="4">{}</textarea>
    <label>Response Body</label>
    <textarea id="resp-body" rows="6"></textarea>
  </div>

  <div class="actions">
    <button class="btn" onclick="forward()">▶ Forward</button>
    <button class="btn btn-danger" onclick="drop()">✕ Drop</button>
    <button class="btn btn-secondary" onclick="toggleRespond()">↩ Respond with...</button>
    <button class="btn hidden" id="send-respond" onclick="sendRespond()">Send Response</button>
  </div>

  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();

    // Set initial method selection
    const methodEl = document.getElementById('method');
    const initMethod = ${JSON.stringify(req.method || 'GET')};
    for (let i = 0; i < methodEl.options.length; i++) {
      if (methodEl.options[i].value === initMethod) { methodEl.selectedIndex = i; break; }
    }

    function getHeaders() {
      try { return JSON.parse(document.getElementById('headers').value); }
      catch(e) { return {}; }
    }

    function forward() {
      vscode.postMessage({ type: 'forward', data: {
        method: document.getElementById('method').value,
        url: document.getElementById('url').value,
        headers: getHeaders(),
        body: document.getElementById('body').value,
      }});
    }

    function drop() {
      vscode.postMessage({ type: 'drop', data: {} });
    }

    function toggleRespond() {
      const section = document.getElementById('respond-section');
      const sendBtn = document.getElementById('send-respond');
      section.classList.toggle('visible');
      sendBtn.style.display = section.classList.contains('visible') ? '' : 'none';
    }

    function sendRespond() {
      let respHeaders = {};
      try { respHeaders = JSON.parse(document.getElementById('resp-headers').value); } catch(e) {}
      vscode.postMessage({ type: 'respond', data: {
        statusCode: parseInt(document.getElementById('resp-status').value, 10) || 200,
        statusText: document.getElementById('resp-status-text').value || 'OK',
        headers: respHeaders,
        body: document.getElementById('resp-body').value,
      }});
    }
  </script>
</body>
</html>`;
    }
}

