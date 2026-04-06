import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { TrafficPanel } from './trafficPanel';
import { BreakpointsProvider, BreakpointItem } from './breakpointsProvider';
import { TrafficProvider, TrafficItem } from './trafficProvider';
import { EngineProcessManager } from './engineProcessManager';
import { ReplayPanel } from './replayPanel';
import { RequestEditor } from './requestEditor';

let engineClient: EngineClient | undefined;
let processManager: EngineProcessManager | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
let pausedRequestsStream: { cancel: () => void } | undefined;

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

    processManager = new EngineProcessManager(context);
    engineClient = new EngineClient(host, grpcPort, tlsEnabled, authToken);

    const trafficProvider = new TrafficProvider(engineClient);
    const breakpointsProvider = new BreakpointsProvider(engineClient);

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
            startEngine(context, processManager!, engineClient!, breakpointsProvider, trafficProvider)
        ),
        vscode.commands.registerCommand('apix.stopEngine', () =>
            stopEngine(processManager!)
        ),
        vscode.commands.registerCommand('apix.clearHistory', async () => {
            try {
                await engineClient?.clearHistory();
                trafficProvider.refresh();
            } catch (err: any) {
                vscode.window.showErrorMessage(`APiX: Failed to clear history — ${err?.message || err}`);
            }
        }),
        vscode.commands.registerCommand('apix.addBreakpoint', () =>
            addBreakpoint(engineClient!, breakpointsProvider)
        ),
        vscode.commands.registerCommand('apix.deleteBreakpoint', (itemOrId: BreakpointItem | string) => {
            const id = typeof itemOrId === 'string' ? itemOrId : itemOrId?.rule?.id;
            if (id) { deleteBreakpoint(engineClient!, breakpointsProvider, id); }
        }),
        vscode.commands.registerCommand('apix.toggleBreakpoint', async (id: string) => {
            if (!id || !engineClient) { return; }
            try {
                const list = await engineClient.listBreakpoints();
                const rule = list.breakpoints?.find(b => b.id === id);
                if (rule) {
                    await engineClient.setBreakpoint({ ...rule, enabled: !rule.enabled });
                    breakpointsProvider.refresh();
                }
            } catch (err: any) {
                vscode.window.showErrorMessage(`APiX: Toggle failed — ${err?.message || err}`);
            }
        }),
        vscode.commands.registerCommand('apix.replayRequest', (itemOrId: TrafficItem | string) => {
            const id = typeof itemOrId === 'string' ? itemOrId : itemOrId?.transaction?.id;
            if (id) { openReplayPanel(context, engineClient!, id); }
        }),
        vscode.commands.registerCommand('apix.openTrafficPanel', () =>
            TrafficPanel.createOrShow(context.extensionUri, engineClient!)
        ),
        vscode.commands.registerCommand('apix.refreshTraffic', () => {
            trafficProvider.refresh();
        }),
    );

    if (autoStart) {
        await startEngine(context, processManager, engineClient, breakpointsProvider, trafficProvider);
    }
}

export function deactivate(): void {
    pausedRequestsStream?.cancel();
    processManager?.stop();
    engineClient?.close();
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
                // Silently ignore — engine may not be running
                console.warn('[APiX] WatchPausedRequests stream error:', err?.message);
            },
            () => {
                console.log('[APiX] WatchPausedRequests stream ended.');
            }
        );
        pausedRequestsStream = stream;
    } catch (err) {
        console.warn('[APiX] Could not start WatchPausedRequests:', err);
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
