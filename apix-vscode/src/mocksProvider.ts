import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { RewriteRule } from './types';
import { Logger } from './logger';

/**
 * MocksProvider implements TreeDataProvider for the APiX Mocks view.
 * Each item represents one RewriteRule registered in the engine.
 */
export class MocksProvider implements vscode.TreeDataProvider<MockItem | ErrorItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<MockItem | ErrorItem | undefined | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    constructor(private readonly client: EngineClient, private readonly logger: Logger) {}

    /** Force a refresh of the tree view. */
    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    dispose(): void {
        this._onDidChangeTreeData.dispose();
    }

    getTreeItem(element: MockItem | ErrorItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: MockItem | ErrorItem): Promise<(MockItem | ErrorItem)[]> {
        if (element) {
            return [];
        }
        try {
            const list = await this.client.listRewriteRules();
            return (list.rules || []).map(rule => new MockItem(rule));
        } catch (err: any) {
            const msg = err?.message || String(err);
            this.logger.error('Mocks view error', { message: msg });
            return [new ErrorItem(`Engine unreachable: ${msg}`)];
        }
    }
}

/** A single item in the Mocks tree view. */
export class MockItem extends vscode.TreeItem {
    constructor(public readonly rule: RewriteRule) {
        super(rule.name || rule.id, vscode.TreeItemCollapsibleState.None);

        this.description = rule.action || '';
        this.tooltip = `Action: ${rule.action}\nEnabled: ${rule.enabled}\nPriority: ${rule.priority}`;
        this.contextValue = 'mockRule';

        this.iconPath = rule.enabled
            ? new vscode.ThemeIcon('circle-filled')
            : new vscode.ThemeIcon('circle-outline');
    }
}

/** Error item displayed when the engine is unreachable. */
export class ErrorItem extends vscode.TreeItem {
    constructor(message: string) {
        super(message, vscode.TreeItemCollapsibleState.None);
        this.iconPath = new vscode.ThemeIcon('error');
        this.description = '';
    }
}
