import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { TrafficPanel } from './trafficPanel';
import { BreakpointsProvider, BreakpointItem } from './breakpointsProvider';
import { TrafficProvider, TrafficItem } from './trafficProvider';
import { EngineProcessManager } from './engineProcessManager';
import { ReplayPanel } from './replayPanel';
import { RequestEditor } from './requestEditor';
import { HttpTransaction } from './types';
import { buildCurlCommand } from './trafficFormats';

let engineClient: EngineClient | undefined;
let processManager: EngineProcessManager | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
let pausedRequestsStream: { cancel: () => void } | undefined;
let trafficProvider: TrafficProvider | undefined;
let breakpointsProvider: BreakpointsProvider | undefined;
let outputChannel: vscode.OutputChannel | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
    const config = vscode.workspace.getConfiguration('apix');
    const host: string = config.get('engine.host', 'localhost');
    const grpcPort: number = config.get('engine.grpcPort', 9090);
    const autoStart: boolean = config.get('engine.autoStart', true);
    const tlsEnabled: boolean = config.get('engine.tlsEnabled', false);
    const authToken: string = config.get('engine.authToken', '');

    // Status bar item
    statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBarItem.text = '$(circle-outline) APiX: Stopped';
    statusBarItem.tooltip = 'APiX HTTP Proxy';
    statusBarItem.command = 'apix.startEngine';
    statusBarItem.show();
    context.subscriptions.push(statusBarItem);

    // Output channel for extension logging and error reporting
    outputChannel = vscode.window.createOutputChannel('APiX');
    context.subscriptions.push(outputChannel);

    processManager = new EngineProcessManager(context);
    engineClient = new EngineClient(host, grpcPort, tlsEnabled, authToken);

    trafficProvider = new TrafficProvider(engineClient, outputChannel);
    breakpointsProvider = new BreakpointsProvider(engineClient, outputChannel);

    const trafficViewDisposable = vscode.window.registerTreeDataProvider(
        'apix.trafficView',
        trafficProvider
    );

    const breakpointsViewDisposable = vscode.window.registerTreeDataProvider(
        'apix.breakpointsView',
        breakpointsProvider
    );

    context.subscriptions.push(
        trafficViewDisposable,
        breakpointsViewDisposable,

        vscode.commands.registerCommand('apix.startEngine', () =>
            startEngine(context, processManager!, engineClient!, breakpointsProvider!, trafficProvider!)
        ),
        vscode.commands.registerCommand('apix.stopEngine', () =>
            stopEngine(processManager!)
        ),
        vscode.commands.registerCommand('apix.clearHistory', async () => {
            try {
                await engineClient?.clearHistory();
                trafficProvider?.refresh();
            } catch (err: any) {
                vscode.window.showErrorMessage(`APiX: Failed to clear history — ${err?.message || err}`);
            }
        }),
        vscode.commands.registerCommand('apix.addBreakpoint', () =>
            addBreakpoint(engineClient!, breakpointsProvider!)
        ),
        vscode.commands.registerCommand('apix.deleteBreakpoint', (itemOrId: BreakpointItem | string) => {
            const id = typeof itemOrId === 'string' ? itemOrId : itemOrId?.rule?.id;
            if (id) { deleteBreakpoint(engineClient!, breakpointsProvider!, id); }
        }),
        vscode.commands.registerCommand('apix.toggleBreakpoint', async (id: string) => {
            if (!id || !engineClient) { return; }
            try {
                const list = await engineClient.listBreakpoints();
                const rule = list.breakpoints?.find(b => b.id === id);
                if (rule) {
                    await engineClient.setBreakpoint({ ...rule, enabled: !rule.enabled });
                    breakpointsProvider?.refresh();
                }
            } catch (err: any) {
                vscode.window.showErrorMessage(`APiX: Toggle failed — ${err?.message || err}`);
            }
        }),
        vscode.commands.registerCommand('apix.replayRequest', (itemOrId: TrafficItem | string) => {
            const id = typeof itemOrId === 'string' ? itemOrId : itemOrId?.transaction?.id;
            if (id) { openReplayPanel(context, engineClient!, id); }
        }),
        vscode.commands.registerCommand('apix.copyAsCurl', (itemOrTx: TrafficItem | HttpTransaction) =>
            copyAsCurl(itemOrTx)
        ),
        vscode.commands.registerCommand('apix.exportHAR', (itemOrTx?: TrafficItem | HttpTransaction) =>
            exportHAR(engineClient!, itemOrTx)
        ),
        vscode.commands.registerCommand('apix.importHAR', () =>
            importHAR(engineClient!, trafficProvider!)
        ),
        vscode.commands.registerCommand('apix.openTrafficPanel', () =>
            TrafficPanel.createOrShow(context.extensionUri, engineClient!)
        ),
        vscode.commands.registerCommand('apix.refreshTraffic', () => {
            trafficProvider?.refresh();
        }),
    );

    if (autoStart) {
        // Start engine asynchronously to avoid blocking extension activation.
        // Errors are logged to the output channel so the user can inspect them.
        startEngine(context, processManager!, engineClient!, breakpointsProvider!, trafficProvider!)
            .catch(err => outputChannel?.appendLine(`APiX: auto-start failed — ${err?.message || err}`));
    }
}

export function deactivate(): void {
    // Cancel any active streams
    pausedRequestsStream?.cancel();
    pausedRequestsStream = undefined;
    
    // Stop engine process
    processManager?.stop();
    
    // Dispose all providers and their EventEmitters
    trafficProvider?.dispose();
    breakpointsProvider?.dispose();
    
    // Dispose webview panels
    TrafficPanel.currentPanel?.dispose();
    
    // Close gRPC client
    engineClient?.close();
    
    // Dispose status bar item
    statusBarItem?.dispose();

    // Dispose output channel
    outputChannel?.dispose();
}

async function startEngine(
    context: vscode.ExtensionContext,
    pm: EngineProcessManager,
    client: EngineClient,
    breakpointsProvider: BreakpointsProvider,
    trafficProvider: TrafficProvider
): Promise<void> {
    if (pm.isRunning()) {
        vscode.window.showInformationMessage('APiX Engine is already running.');
        return;
    }
    try {
        statusBarItem!.text = '$(sync~spin) APiX: Starting...';
        await pm.start();

        // Brief delay to let the gRPC server initialize
        await new Promise(resolve => setTimeout(resolve, 1000));

        // Verify the engine is reachable
        try {
            await client.getStatus();
        } catch {
            // Engine might not be ready yet — not a fatal error
        }

        statusBarItem!.text = '$(circle-filled) APiX: Running';
        statusBarItem!.tooltip = 'APiX: Running — click to open traffic panel';
        statusBarItem!.command = 'apix.openTrafficPanel';

        breakpointsProvider.refresh();
        trafficProvider.refresh();

        // Start watching for paused (breakpointed) requests
        _startWatchPausedRequests(context, client);

    } catch (err: any) {
        statusBarItem!.text = '$(error) APiX: Error';
        vscode.window.showErrorMessage(`APiX: Failed to start engine — ${err?.message || err}`);
    }
}

async function stopEngine(pm: EngineProcessManager): Promise<void> {
    pausedRequestsStream?.cancel();
    pausedRequestsStream = undefined;
    pm.stop();
    statusBarItem!.text = '$(circle-outline) APiX: Stopped';
    statusBarItem!.tooltip = 'APiX HTTP Proxy — click to start';
    statusBarItem!.command = 'apix.startEngine';
    vscode.window.showInformationMessage('APiX Engine stopped.');
}

function _startWatchPausedRequests(context: vscode.ExtensionContext, client: EngineClient): void {
    try {
        pausedRequestsStream?.cancel();
        const stream = client.watchPausedRequests(
            (paused) => {
                RequestEditor.showEditor(context.extensionUri, client, paused);
            },
            (err) => {
                // Log to output channel so user can inspect stream errors
                outputChannel?.appendLine(`[APiX] WatchPausedRequests stream error: ${err?.message || err}`);
            },
            () => {
                console.log('[APiX] WatchPausedRequests stream ended.');
            }
        );
        pausedRequestsStream = stream;
    } catch (err) {
        outputChannel?.appendLine(`[APiX] Could not start WatchPausedRequests: ${err}`);
    }
}

async function addBreakpoint(client: EngineClient, provider: BreakpointsProvider): Promise<void> {
    const urlPattern = await vscode.window.showInputBox({
        title: 'APiX: Add Breakpoint',
        prompt: 'Enter URL pattern (regex or substring)',
        placeHolder: 'e.g. /api/users or https://example.com/.*',
        validateInput: v => v.trim() ? undefined : 'Pattern cannot be empty',
    });
    if (!urlPattern) { return; }

    const methodItems = ['ALL', 'GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];
    const picked = await vscode.window.showQuickPick(methodItems, {
        title: 'APiX: Select HTTP methods to intercept',
        canPickMany: true,
        placeHolder: 'Select methods (select ALL for any method)',
    });
    if (!picked) { return; }

    const methods = picked.includes('ALL') ? [] : picked;

    try {
        await client.setBreakpoint({
            id: '',
            urlPattern: urlPattern.trim(),
            methods,
            enabled: true,
        });
        provider.refresh();
        vscode.window.showInformationMessage(`APiX: Breakpoint added for "${urlPattern}"`);
    } catch (err: any) {
        vscode.window.showErrorMessage(`APiX: Failed to add breakpoint — ${err?.message || err}`);
    }
}

async function deleteBreakpoint(client: EngineClient, provider: BreakpointsProvider, id: string): Promise<void> {
    try {
        await client.deleteBreakpoint(id);
        provider.refresh();
    } catch (err: any) {
        vscode.window.showErrorMessage(`APiX: Failed to delete breakpoint — ${err?.message || err}`);
    }
}

async function openReplayPanel(
    context: vscode.ExtensionContext,
    client: EngineClient,
    requestId: string
): Promise<void> {
    await ReplayPanel.show(context.extensionUri, client, requestId);
}

async function copyAsCurl(itemOrTx: TrafficItem | HttpTransaction): Promise<void> {
    const tx = itemOrTx instanceof TrafficItem ? itemOrTx.transaction : itemOrTx;
    if (!tx?.request?.url) {
        vscode.window.showErrorMessage('APiX: No request available to copy as curl.');
        return;
    }

    await vscode.env.clipboard.writeText(buildCurlCommand(tx));
    vscode.window.showInformationMessage('APiX: Copied request as curl.');
}

async function exportHAR(client: EngineClient, itemOrTx?: TrafficItem | HttpTransaction): Promise<void> {
    const tx = itemOrTx instanceof TrafficItem ? itemOrTx.transaction : itemOrTx;
    const defaultDir = vscode.workspace.workspaceFolders?.[0]?.uri ?? vscode.Uri.file(process.cwd());
    const uri = await vscode.window.showSaveDialog({
        title: 'APiX: Export Traffic as HAR',
        defaultUri: vscode.Uri.joinPath(defaultDir, tx?.id ? `${tx.id}.har` : 'apix-traffic.har'),
        filters: { 'HAR Files': ['har'], 'JSON Files': ['json'] },
    });
    if (!uri) { return; }

    try {
        const harJson = await client.exportHAR(tx?.id ? [tx.id] : []);
        await vscode.workspace.fs.writeFile(uri, Buffer.from(harJson, 'utf8'));
        vscode.window.showInformationMessage(`APiX: Exported HAR to ${uri.fsPath}.`);
    } catch (err: any) {
        vscode.window.showErrorMessage(`APiX: Failed to export HAR — ${err?.message || err}`);
    }
}

async function importHAR(client: EngineClient, provider: TrafficProvider): Promise<void> {
    const picks = await vscode.window.showOpenDialog({
        title: 'APiX: Import HAR File',
        canSelectMany: false,
        filters: { 'HAR Files': ['har', 'json'] },
    });
    if (!picks || picks.length === 0) { return; }

    try {
        const data = await vscode.workspace.fs.readFile(picks[0]);
        const importedIDs = await client.importHAR(Buffer.from(data).toString('utf8'));
        provider.refresh();
        vscode.window.showInformationMessage(`APiX: Imported ${importedIDs.length} HAR entr${importedIDs.length === 1 ? 'y' : 'ies'}.`);
    } catch (err: any) {
        vscode.window.showErrorMessage(`APiX: Failed to import HAR — ${err?.message || err}`);
    }
}
