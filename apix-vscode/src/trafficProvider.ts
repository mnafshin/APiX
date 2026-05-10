import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { HttpTransaction } from './types';
import { isWebSocketTransaction } from './trafficFormats';
import { Logger } from './logger';

/**
 * TrafficProvider implements TreeDataProvider for the APiX Traffic tree view.
 * Shows the most recent N transactions as tree items.
 */
export class TrafficProvider implements vscode.TreeDataProvider<TrafficItem | ErrorItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<TrafficItem | ErrorItem | undefined | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    private readonly maxItems: number;
    private readonly refreshDebounceMs = 200;
    private pendingRefresh = false;
    private refreshTimer: ReturnType<typeof setTimeout> | undefined;
    private captureStream: { cancel: () => void } | undefined;
    private captureRetryTimer: ReturnType<typeof setTimeout> | undefined;
    private captureRetryDelayMs = 1000;

    constructor(private readonly client: EngineClient, private readonly logger: Logger) {
        const config = vscode.workspace.getConfiguration('apix');
        this.maxItems = config.get<number>('traffic.maxItems', 500);
        this.startCapture();
    }

    refresh(): void {
        this.scheduleRefresh();
    }

    dispose(): void {
        if (this.refreshTimer) {
            clearTimeout(this.refreshTimer);
            this.refreshTimer = undefined;
        }
        if (this.captureRetryTimer) {
            clearTimeout(this.captureRetryTimer);
            this.captureRetryTimer = undefined;
        }
        this.captureStream?.cancel();
        this.captureStream = undefined;
        this._onDidChangeTreeData.dispose();
    }

    getTreeItem(element: TrafficItem | ErrorItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: TrafficItem | ErrorItem): Promise<(TrafficItem | ErrorItem)[]> {
        if (element) { return []; }
        try {
            const [txs, _cancel] = await this.client.getHistory({
                limit: this.maxItems,
                offset: 0,
                urlFilter: '',
                methodFilter: '',
                statusFilter: 0,
                sinceMs: 0,
            });
            return txs.map(tx => new TrafficItem(tx));
        } catch (err: any) {
            const msg = err?.message || String(err);
            this.logger.error('Traffic view error', { message: msg });
            return [new ErrorItem(`Connection lost: ${msg}`, 'apix.refreshTraffic')];
        }
    }

    private startCapture(): void {
        if (this.captureRetryTimer) {
            clearTimeout(this.captureRetryTimer);
            this.captureRetryTimer = undefined;
        }
        this.captureStream?.cancel();
        this.captureStream = undefined;
        try {
            const stream = this.client.captureTraffic(
                () => {
                    this.captureRetryDelayMs = 1000;
                    this.scheduleRefresh();
                },
                (err) => {
                    this.logger.error('Traffic stream error', { message: err?.message || String(err) });
                    this.scheduleCaptureRetry();
                },
                () => this.scheduleCaptureRetry()
            );
            this.captureStream = { cancel: () => stream.cancel() };
        } catch (err) {
            this.logger.error('Could not start traffic stream', { message: String(err) });
            this.scheduleCaptureRetry();
        }
    }

    private scheduleCaptureRetry(): void {
        if (this.captureRetryTimer) {
            return;
        }
        const delay = this.captureRetryDelayMs;
        this.captureRetryDelayMs = Math.min(this.captureRetryDelayMs * 2, 30000);
        this.captureRetryTimer = setTimeout(() => {
            this.captureRetryTimer = undefined;
            this.startCapture();
        }, delay);
    }

    private scheduleRefresh(): void {
        if (this.pendingRefresh) {
            return;
        }
        this.pendingRefresh = true;
        this.refreshTimer = setTimeout(() => {
            this.pendingRefresh = false;
            this.refreshTimer = undefined;
            this._onDidChangeTreeData.fire(undefined);
        }, this.refreshDebounceMs);
    }
}

/** A single item in the Traffic tree view, representing one HTTP transaction. */
export class TrafficItem extends vscode.TreeItem {
    public readonly transaction: HttpTransaction;

    constructor(tx: HttpTransaction) {
        const method = tx.request?.method || 'GET';
        const url = tx.request?.url || '(unknown)';
        const isWebSocket = isWebSocketTransaction(tx);
        super(`${isWebSocket ? '[WS] ' : ''}${method} ${url}`, vscode.TreeItemCollapsibleState.None);

        this.transaction = tx;
        const status = tx.response?.statusCode || 0;
        const duration = tx.durationMs ? `${tx.durationMs}ms` : '';
        const summary = status ? `${status} ${duration}`.trim() : duration || '—';
        this.description = isWebSocket ? `WS ${summary}`.trim() : summary;
        this.tooltip = new vscode.MarkdownString(
            `**${isWebSocket ? 'WS ' : ''}${method}** ${url}\n\nStatus: ${status || '—'}  Duration: ${duration || '—'}\n\nRequest ID: ${tx.requestId || tx.id || '—'}`
        );
        this.contextValue = 'httpTransaction';
        this.iconPath = this._iconForStatus(status);
    }

    private _iconForStatus(status: number): vscode.ThemeIcon {
        if (status >= 500) { return new vscode.ThemeIcon('circle-filled', new vscode.ThemeColor('charts.red')); }
        if (status >= 400) { return new vscode.ThemeIcon('circle-filled', new vscode.ThemeColor('charts.orange')); }
        if (status >= 300) { return new vscode.ThemeIcon('circle-filled', new vscode.ThemeColor('charts.blue')); }
        if (status >= 200) { return new vscode.ThemeIcon('circle-filled', new vscode.ThemeColor('charts.green')); }
        return new vscode.ThemeIcon('circle-outline');
    }
}

/** Error item displayed when the engine is unreachable. */
export class ErrorItem extends vscode.TreeItem {
    constructor(message: string, refreshCommand?: string) {
        super(message, vscode.TreeItemCollapsibleState.None);
        this.iconPath = new vscode.ThemeIcon('error');
        this.description = refreshCommand ? 'Click to retry' : '';
        if (refreshCommand) {
            this.command = {
                command: refreshCommand,
                title: 'Retry',
            };
        }
    }
}
