import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { HttpTransaction } from './types';

/**
 * TrafficProvider implements TreeDataProvider for the APiX Traffic tree view.
 * Shows the most recent N transactions as tree items.
 */
export class TrafficProvider implements vscode.TreeDataProvider<TrafficItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<TrafficItem | undefined | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    private readonly maxItems: number;

    constructor(private readonly client: EngineClient) {
        const config = vscode.workspace.getConfiguration('apix');
        this.maxItems = config.get<number>('traffic.maxItems', 500);
    }

    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    dispose(): void {
        this._onDidChangeTreeData.dispose();
    }

    getTreeItem(element: TrafficItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: TrafficItem): Promise<TrafficItem[]> {
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
        } catch {
            return [];
        }
    }
}

/** A single item in the Traffic tree view, representing one HTTP transaction. */
export class TrafficItem extends vscode.TreeItem {
    public readonly transaction: HttpTransaction;

    constructor(tx: HttpTransaction) {
        const method = tx.request?.method || 'GET';
        const url = tx.request?.url || '(unknown)';
        super(`${method} ${url}`, vscode.TreeItemCollapsibleState.None);

        this.transaction = tx;
        const status = tx.response?.statusCode || 0;
        const duration = tx.durationMs ? `${tx.durationMs}ms` : '';
        this.description = status ? `${status} ${duration}`.trim() : duration || '—';
        this.tooltip = new vscode.MarkdownString(
            `**${method}** ${url}\n\nStatus: ${status || '—'}  Duration: ${duration || '—'}`
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
