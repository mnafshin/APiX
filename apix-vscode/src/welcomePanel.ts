import * as vscode from 'vscode';
import { EngineClient } from './engineClient';

const HIDE_WELCOME_KEY = 'apix.hideWelcome';

export class WelcomePanel {
    private static currentPanel: WelcomePanel | undefined;
    private static readonly viewType = 'apixWelcome';

    private readonly panel: vscode.WebviewPanel;
    private readonly context: vscode.ExtensionContext;
    private readonly client: EngineClient;
    private readonly disposables: vscode.Disposable[] = [];

    static show(context: vscode.ExtensionContext, client: EngineClient): void {
        const column = vscode.window.activeTextEditor?.viewColumn ?? vscode.ViewColumn.One;
        if (WelcomePanel.currentPanel) {
            WelcomePanel.currentPanel.panel.reveal(column);
            void WelcomePanel.currentPanel.postStatus();
            return;
        }

        const panel = vscode.window.createWebviewPanel(
            WelcomePanel.viewType,
            'APiX Welcome',
            column,
            { enableScripts: true }
        );

        WelcomePanel.currentPanel = new WelcomePanel(panel, context, client);
    }

    private constructor(panel: vscode.WebviewPanel, context: vscode.ExtensionContext, client: EngineClient) {
        this.panel = panel;
        this.context = context;
        this.client = client;

        this.panel.webview.html = this.getHtml();
        this.panel.onDidDispose(() => this.dispose(), null, this.disposables);
        this.panel.webview.onDidReceiveMessage((msg) => {
            void this.handleMessage(msg);
        }, null, this.disposables);
        void this.postStatus();
    }

    private async handleMessage(msg: { type?: string; data?: any }): Promise<void> {
        switch (msg.type) {
            case 'startEngine':
                await vscode.commands.executeCommand('apix.startEngine');
                await this.postStatus();
                return;
            case 'openTraffic':
                await vscode.commands.executeCommand('apix.openTrafficPanel');
                return;
            case 'copyProxyEnv': {
                const proxyPort = vscode.workspace.getConfiguration('apix').get<number>('engine.proxyPort', 8080);
                const text = `export HTTP_PROXY=http://localhost:${proxyPort}\nexport HTTPS_PROXY=http://localhost:${proxyPort}`;
                await vscode.env.clipboard.writeText(text);
                void vscode.window.showInformationMessage('APiX: Proxy environment variables copied.');
                return;
            }
            case 'runTestRequest': {
                const proxyPort = vscode.workspace.getConfiguration('apix').get<number>('engine.proxyPort', 8080);
                const terminal = vscode.window.createTerminal('APiX Test Request');
                terminal.show();
                terminal.sendText(`curl -x http://localhost:${proxyPort} https://example.com`, true);
                return;
            }
            case 'openDocs':
                await vscode.env.openExternal(vscode.Uri.parse('https://github.com/mnafshin/APiX#readme'));
                return;
            case 'setHideWelcome': {
                const hide = Boolean(msg.data?.hide);
                await this.context.globalState.update(HIDE_WELCOME_KEY, hide);
                return;
            }
            case 'refreshStatus':
                await this.postStatus();
                return;
            default:
                return;
        }
    }

    private async postStatus(): Promise<void> {
        let engineRunning = false;
        try {
            await this.client.getStatus();
            engineRunning = true;
        } catch {
            engineRunning = false;
        }
        const hideWelcome = this.context.globalState.get<boolean>(HIDE_WELCOME_KEY, false);
        await this.panel.webview.postMessage({
            type: 'status',
            data: { engineRunning, hideWelcome },
        });
    }

    private getHtml(): string {
        const nonce = this.getNonce();
        return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-${nonce}'; style-src 'unsafe-inline';">
  <title>Welcome to APiX</title>
  <style>
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground); background: var(--vscode-editor-background); padding: 16px; }
    .card { border: 1px solid var(--vscode-panel-border); border-radius: 8px; padding: 16px; max-width: 800px; }
    h1 { margin: 0 0 12px; font-size: 20px; }
    .muted { color: var(--vscode-descriptionForeground); margin-bottom: 16px; }
    .step { display: flex; justify-content: space-between; align-items: center; padding: 10px 0; border-top: 1px solid var(--vscode-panel-border); gap: 12px; }
    .step:first-of-type { border-top: none; }
    .status-ok { color: var(--vscode-testing-iconPassed); font-weight: 600; }
    .status-warn { color: var(--vscode-testing-iconFailed); font-weight: 600; }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 16px; }
    button { background: var(--vscode-button-background); color: var(--vscode-button-foreground); border: none; padding: 6px 12px; border-radius: 4px; cursor: pointer; }
    button:hover { background: var(--vscode-button-hoverBackground); }
    label { display: inline-flex; align-items: center; gap: 8px; margin-top: 14px; color: var(--vscode-descriptionForeground); }
  </style>
</head>
<body>
  <div class="card">
    <h1>Welcome to APiX</h1>
    <div class="muted">Set up the proxy quickly, validate engine connectivity, and run your first captured request.</div>

    <div class="step">
      <div><strong>Step 1:</strong> Engine status</div>
      <div id="engine-status" class="status-warn">Checking...</div>
    </div>
    <div class="step">
      <div><strong>Step 2:</strong> Configure proxy for your shell/tools</div>
      <button id="copy-proxy">Copy Proxy Env</button>
    </div>
    <div class="step">
      <div><strong>Step 3:</strong> Send a test request through APiX</div>
      <button id="run-test">Run Test Request</button>
    </div>

    <div class="actions">
      <button id="start-engine">Start Engine</button>
      <button id="open-traffic">Open Traffic Inspector</button>
      <button id="refresh-status">Refresh Status</button>
      <button id="open-docs">Open Docs</button>
    </div>

    <label>
      <input type="checkbox" id="hide-welcome" />
      Don't show this again
    </label>
  </div>

  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    const engineStatusEl = document.getElementById('engine-status');
    const hideWelcomeEl = document.getElementById('hide-welcome');

    function send(type, data) {
      vscode.postMessage({ type, data: data || {} });
    }

    document.getElementById('start-engine').addEventListener('click', () => send('startEngine'));
    document.getElementById('open-traffic').addEventListener('click', () => send('openTraffic'));
    document.getElementById('copy-proxy').addEventListener('click', () => send('copyProxyEnv'));
    document.getElementById('run-test').addEventListener('click', () => send('runTestRequest'));
    document.getElementById('open-docs').addEventListener('click', () => send('openDocs'));
    document.getElementById('refresh-status').addEventListener('click', () => send('refreshStatus'));
    hideWelcomeEl.addEventListener('change', () => send('setHideWelcome', { hide: hideWelcomeEl.checked }));

    window.addEventListener('message', (event) => {
      const msg = event.data;
      if (msg.type !== 'status') return;
      const isRunning = Boolean(msg.data && msg.data.engineRunning);
      const hide = Boolean(msg.data && msg.data.hideWelcome);
      engineStatusEl.textContent = isRunning ? 'Running' : 'Not running';
      engineStatusEl.className = isRunning ? 'status-ok' : 'status-warn';
      hideWelcomeEl.checked = hide;
    });
  </script>
</body>
</html>`;
    }

    private getNonce(): string {
        const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
        let text = '';
        for (let i = 0; i < 32; i++) {
            text += chars.charAt(Math.floor(Math.random() * chars.length));
        }
        return text;
    }

    public dispose(): void {
        WelcomePanel.currentPanel = undefined;
        this.panel.dispose();
        while (this.disposables.length) {
            this.disposables.pop()?.dispose();
        }
    }
}
