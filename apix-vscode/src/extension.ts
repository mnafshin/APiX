import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { TrafficPanel } from './trafficPanel';
import { BreakpointsProvider, BreakpointItem } from './breakpointsProvider';
import { TrafficProvider, TrafficItem, ErrorItem } from './trafficProvider';
import { MocksProvider, MockItem } from './mocksProvider';
import { EngineProcessManager } from './engineProcessManager';
import { ReplayPanel } from './replayPanel';
import { RequestEditor } from './requestEditor';
import { HttpTransaction } from './types';
import { buildCurlCommand } from './trafficFormats';
import { WelcomePanel } from './welcomePanel';
import { Logger } from './logger';

const AUTH_TOKEN_SECRET_KEY = 'apix.authToken';
const WELCOME_SHOWN_KEY = 'apix.welcomeShown';
const HIDE_WELCOME_KEY = 'apix.hideWelcome';

let engineClient: EngineClient | undefined;
let processManager: EngineProcessManager | undefined;
let statusBarItem: vscode.StatusBarItem | undefined;
let pausedRequestsStream: { cancel: () => void } | undefined;
let trafficProvider: TrafficProvider | undefined;
let breakpointsProvider: BreakpointsProvider | undefined;
let mocksProvider: MocksProvider | undefined;
let outputChannel: vscode.OutputChannel | undefined;
let logger: Logger | undefined;
let pausedRetryTimer: ReturnType<typeof setTimeout> | undefined;
let pausedRetryDelayMs = 1000;
let trafficTreeView: vscode.TreeView<TrafficItem | ErrorItem> | undefined;
let selectedTrafficItem: TrafficItem | undefined;

export async function activate(context: vscode.ExtensionContext): Promise<void> {
    const config = vscode.workspace.getConfiguration('apix');
    const host: string = config.get('engine.host', 'localhost');
    const grpcPort: number = config.get('engine.grpcPort', 9090);
    const autoStart: boolean = config.get('engine.autoStart', true);
    const tlsEnabled: boolean = config.get('engine.tlsEnabled', false);

    // Status bar item
    statusBarItem = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 100);
    statusBarItem.text = '$(circle-outline) APiX: Stopped';
    statusBarItem.tooltip = 'APiX HTTP Proxy';
    statusBarItem.command = 'apix.startEngine';
    statusBarItem.show();
    context.subscriptions.push(statusBarItem);

    // Output channel for extension logging and error reporting
    outputChannel = vscode.window.createOutputChannel('APiX');
    logger = new Logger(outputChannel, 'extension');
    context.subscriptions.push(outputChannel);

    processManager = new EngineProcessManager(context, logger.child('engineProcess'));
    const authToken = await resolveAuthToken(context, config, logger.child('auth'));
    engineClient = new EngineClient(host, grpcPort, tlsEnabled, authToken, logger.child('engineClient'));
    processManager.onUnexpectedExit = (message: string) => {
        statusBarItem!.text = '$(debug-disconnect) APiX: Disconnected';
        statusBarItem!.tooltip = message;
        statusBarItem!.command = 'apix.startEngine';
    };
    processManager.onRestarting = (attempt: number, delayMs: number) => {
        statusBarItem!.text = '$(sync~spin) APiX: Reconnecting...';
        statusBarItem!.tooltip = `APiX: reconnect attempt ${attempt} in ${Math.round(delayMs / 1000)}s`;
        statusBarItem!.command = 'apix.startEngine';
    };
    processManager.onRestart = () => {
        logger?.info('Engine restarted; re-initializing extension streams');
        startEngine(context, processManager!, engineClient!, breakpointsProvider!, trafficProvider!, mocksProvider)
            .catch(err => logger?.error('Reconnect failed', { message: err?.message || String(err) }));
    };

    trafficProvider = new TrafficProvider(engineClient, logger.child('trafficProvider'));
    breakpointsProvider = new BreakpointsProvider(engineClient, logger.child('breakpointsProvider'));
    mocksProvider = new MocksProvider(engineClient, logger.child('mocksProvider'));

    trafficTreeView = vscode.window.createTreeView('apix.trafficView', {
        treeDataProvider: trafficProvider,
        canSelectMany: false,
    });
    const trafficSelectionDisposable = trafficTreeView.onDidChangeSelection((e) => {
        selectedTrafficItem = e.selection.find((item): item is TrafficItem => item instanceof TrafficItem);
    });

    const breakpointsViewDisposable = vscode.window.registerTreeDataProvider(
        'apix.breakpointsView',
        breakpointsProvider
    );

    const mocksViewDisposable = vscode.window.registerTreeDataProvider(
        'apix.mocksView',
        mocksProvider
    );

    context.subscriptions.push(
        trafficTreeView,
        trafficSelectionDisposable,
        breakpointsViewDisposable,
        mocksViewDisposable,

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
                TrafficPanel.currentPanel?.clearDisplayedHistory();
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
            const id = typeof itemOrId === 'string'
                ? itemOrId
                : itemOrId?.transaction?.id || selectedTrafficItem?.transaction?.id;
            if (id) { openReplayPanel(context, engineClient!, id); }
        }),
        vscode.commands.registerCommand('apix.composeRequest', () =>
            openReplayPanel(context, engineClient!)
        ),
        vscode.commands.registerCommand('apix.copyAsCurl', (itemOrTx: TrafficItem | HttpTransaction) =>
            copyAsCurl(itemOrTx || selectedTrafficItem)
        ),
        vscode.commands.registerCommand('apix.copyRequestId', (itemOrTxOrID: TrafficItem | HttpTransaction | string) =>
            copyRequestId(itemOrTxOrID)
        ),
        vscode.commands.registerCommand('apix.setAuthToken', () =>
            setAuthToken(context, config, engineClient!)
        ),
        vscode.commands.registerCommand('apix.exportHAR', (itemOrTx?: TrafficItem | HttpTransaction) =>
            exportHAR(engineClient!, itemOrTx)
        ),
        vscode.commands.registerCommand('apix.importHAR', () =>
            importHAR(engineClient!, trafficProvider!)
        ),
        vscode.commands.registerCommand('apix.openTrafficPanel', () =>
            TrafficPanel.createOrShow(context, engineClient!)
        ),
        vscode.commands.registerCommand('apix.showWelcome', () =>
            WelcomePanel.show(context, engineClient!)
        ),
        vscode.commands.registerCommand('apix.refreshTraffic', () => {
            trafficProvider?.refresh();
        }),
        vscode.commands.registerCommand('apix.refreshBreakpoints', () => {
            breakpointsProvider?.refresh();
        }),
        vscode.commands.registerCommand('apix.mocks.refresh', () => {
            mocksProvider?.refresh();
        }),
        vscode.commands.registerCommand('apix.mocks.toggle', async (itemOrId: MockItem | string) => {
            const id = typeof itemOrId === 'string' ? itemOrId : itemOrId?.rule?.id;
            if (!id || !engineClient) { return; }
            try {
                await engineClient.toggleRewriteRule(id);
                mocksProvider?.refresh();
            } catch (err: any) {
                vscode.window.showErrorMessage(`APiX: Toggle mock failed — ${err?.message || err}`);
            }
        }),
        vscode.commands.registerCommand('apix.mocks.delete', async (itemOrId: MockItem | string) => {
            const id = typeof itemOrId === 'string' ? itemOrId : itemOrId?.rule?.id;
            if (!id || !engineClient) { return; }
            try {
                await engineClient.deleteRewriteRule(id);
                mocksProvider?.refresh();
            } catch (err: any) {
                vscode.window.showErrorMessage(`APiX: Delete mock failed — ${err?.message || err}`);
            }
        }),
    );

    if (autoStart) {
        // Start engine asynchronously to avoid blocking extension activation.
        // Errors are logged to the output channel so the user can inspect them.
        startEngine(context, processManager!, engineClient!, breakpointsProvider!, trafficProvider!)
            .catch(err => logger?.error('Auto-start failed', { message: err?.message || String(err) }));

    }

    const hideWelcome = context.globalState.get<boolean>(HIDE_WELCOME_KEY, false);
    const welcomeShown = context.globalState.get<boolean>(WELCOME_SHOWN_KEY, false);
    if (!hideWelcome && !welcomeShown) {
        void context.globalState.update(WELCOME_SHOWN_KEY, true);
        WelcomePanel.show(context, engineClient!);
    }
}

export function deactivate(): void {
    // Cancel any active streams
    pausedRequestsStream?.cancel();
    pausedRequestsStream = undefined;
    if (pausedRetryTimer) {
        clearTimeout(pausedRetryTimer);
        pausedRetryTimer = undefined;
    }
    RequestEditor.closeAll();
    
    // Stop engine process
    processManager?.stop();
    
    // Dispose all providers and their EventEmitters
    trafficProvider?.dispose();
    breakpointsProvider?.dispose();
    mocksProvider?.dispose();
    
    // Dispose webview panels
    TrafficPanel.currentPanel?.dispose();
    
    // Close gRPC client
    engineClient?.close();
    
    // Dispose status bar item
    statusBarItem?.dispose();

    // Dispose output channel
                outputChannel?.dispose();
                logger = undefined;
}

async function startEngine(
    context: vscode.ExtensionContext,
    pm: EngineProcessManager,
    client: EngineClient,
    breakpointsProvider: BreakpointsProvider,
    trafficProvider: TrafficProvider,
    mocksProvider?: MocksProvider
): Promise<void> {
    if (pm.isRunning()) {
        statusBarItem!.text = '$(circle-filled) APiX: Running';
        statusBarItem!.tooltip = 'APiX: Running — click to open unified workspace';
        statusBarItem!.command = 'apix.openTrafficPanel';
        breakpointsProvider.refresh();
        trafficProvider.refresh();
        mocksProvider?.refresh();
        _startWatchPausedRequests(context, client);
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
        statusBarItem!.tooltip = 'APiX: Running — click to open unified workspace';
        statusBarItem!.command = 'apix.openTrafficPanel';

        breakpointsProvider.refresh();
        trafficProvider.refresh();
        mocksProvider?.refresh();

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
    if (pausedRetryTimer) {
        clearTimeout(pausedRetryTimer);
        pausedRetryTimer = undefined;
    }
    RequestEditor.closeAll();
    pm.stop();
    statusBarItem!.text = '$(circle-outline) APiX: Stopped';
    statusBarItem!.tooltip = 'APiX HTTP Proxy — click to start';
    statusBarItem!.command = 'apix.startEngine';
    vscode.window.showInformationMessage('APiX Engine stopped.');
}

function _startWatchPausedRequests(context: vscode.ExtensionContext, client: EngineClient): void {
    try {
        if (pausedRetryTimer) {
            clearTimeout(pausedRetryTimer);
            pausedRetryTimer = undefined;
        }
        pausedRequestsStream?.cancel();
        const stream = client.watchPausedRequests(
            (paused) => {
                pausedRetryDelayMs = 1000;
                RequestEditor.showEditor(context.extensionUri, client, paused);
            },
            (err) => {
                // Log to output channel so user can inspect stream errors
                logger?.error('WatchPausedRequests stream error', { message: err?.message || String(err) });
                RequestEditor.closeAll();
                breakpointsProvider?.refresh();
                if (processManager?.isRunning()) {
                    const delay = pausedRetryDelayMs;
                    pausedRetryDelayMs = Math.min(pausedRetryDelayMs * 2, 30000);
                    pausedRetryTimer = setTimeout(() => {
                        _startWatchPausedRequests(context, client);
                    }, delay);
                }
            },
            () => {
                logger?.warn('WatchPausedRequests stream ended');
                RequestEditor.closeAll();
                breakpointsProvider?.refresh();
                if (processManager?.isRunning()) {
                    const delay = pausedRetryDelayMs;
                    pausedRetryDelayMs = Math.min(pausedRetryDelayMs * 2, 30000);
                    pausedRetryTimer = setTimeout(() => {
                        _startWatchPausedRequests(context, client);
                    }, delay);
                }
            }
        );
        pausedRequestsStream = stream;
    } catch (err) {
        logger?.error('Could not start WatchPausedRequests', { message: String(err) });
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
    requestId?: string
): Promise<void> {
    await ReplayPanel.show(context.extensionUri, client, requestId);
}

async function copyAsCurl(itemOrTx?: TrafficItem | HttpTransaction): Promise<void> {
    const tx = itemOrTx instanceof TrafficItem ? itemOrTx.transaction : itemOrTx;
    if (!tx?.request?.url) {
        vscode.window.showErrorMessage('APiX: No request available to copy as curl.');
        return;
    }

    await vscode.env.clipboard.writeText(buildCurlCommand(tx));
    vscode.window.showInformationMessage('APiX: Copied request as curl.');
}

async function copyRequestId(itemOrTxOrID: TrafficItem | HttpTransaction | string): Promise<void> {
    const requestID = typeof itemOrTxOrID === 'string'
        ? itemOrTxOrID
        : itemOrTxOrID instanceof TrafficItem
            ? (itemOrTxOrID.transaction.requestId || itemOrTxOrID.transaction.id)
            : (itemOrTxOrID.requestId || itemOrTxOrID.id);

    if (!requestID) {
        vscode.window.showErrorMessage('APiX: No request ID available to copy.');
        return;
    }
    await vscode.env.clipboard.writeText(requestID);
    vscode.window.showInformationMessage('APiX: Copied request ID.');
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

async function resolveAuthToken(
    context: vscode.ExtensionContext,
    config: vscode.WorkspaceConfiguration,
    authLogger: Logger
): Promise<string> {
    const legacy = (config.get<string>('engine.authToken', '') || '').trim();
    const secret = (await context.secrets.get(AUTH_TOKEN_SECRET_KEY))?.trim() || '';
    if (secret) {
        return secret;
    }
    if (!legacy) {
        return '';
    }

    await context.secrets.store(AUTH_TOKEN_SECRET_KEY, legacy);
    await clearLegacyAuthTokenSetting(config, authLogger);
    authLogger.info('Migrated apix.engine.authToken to VS Code SecretStorage');
    return legacy;
}

async function clearLegacyAuthTokenSetting(
    config: vscode.WorkspaceConfiguration,
    authLogger?: Logger
): Promise<void> {
    const inspected = config.inspect<string>('engine.authToken');
    const tasks: Promise<void>[] = [];
    if (inspected?.workspaceValue !== undefined) {
        tasks.push(Promise.resolve(config.update('engine.authToken', '', vscode.ConfigurationTarget.Workspace)));
    }
    if (inspected?.workspaceFolderValue !== undefined) {
        tasks.push(Promise.resolve(config.update('engine.authToken', '', vscode.ConfigurationTarget.WorkspaceFolder)));
    }
    if (inspected?.globalValue !== undefined) {
        tasks.push(Promise.resolve(config.update('engine.authToken', '', vscode.ConfigurationTarget.Global)));
    }
    if (tasks.length === 0) {
        return;
    }
    const results = await Promise.allSettled(tasks);
    for (const result of results) {
        if (result.status === 'rejected') {
            authLogger?.error('Failed to clear deprecated apix.engine.authToken setting', {
                reason: String(result.reason),
            });
        }
    }
}

async function setAuthToken(
    context: vscode.ExtensionContext,
    config: vscode.WorkspaceConfiguration,
    client: EngineClient
): Promise<void> {
    const current = (await context.secrets.get(AUTH_TOKEN_SECRET_KEY)) || '';
    const token = await vscode.window.showInputBox({
        title: 'APiX: Set Auth Token',
        prompt: 'Enter the bearer token used for remote engine authentication. Leave empty to clear it.',
        password: true,
        value: current,
    });
    if (token === undefined) {
        return;
    }

    const trimmed = token.trim();
    if (trimmed === '') {
        await context.secrets.delete(AUTH_TOKEN_SECRET_KEY);
        client.setAuthToken('');
        if (logger) {
            await clearLegacyAuthTokenSetting(config, logger.child('auth'));
        }
        vscode.window.showInformationMessage('APiX: Auth token cleared.');
        return;
    }

    await context.secrets.store(AUTH_TOKEN_SECRET_KEY, trimmed);
    client.setAuthToken(trimmed);
    if (logger) {
        await clearLegacyAuthTokenSetting(config, logger.child('auth'));
    }
    vscode.window.showInformationMessage('APiX: Auth token saved to SecretStorage.');
}
