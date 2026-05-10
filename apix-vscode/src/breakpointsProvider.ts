import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { BreakpointRule } from './types';
import { Logger } from './logger';

/**
 * BreakpointsProvider implements TreeDataProvider for the APiX Breakpoints view.
 * Each item represents one BreakpointRule registered in the engine.
 */
export class BreakpointsProvider implements vscode.TreeDataProvider<BreakpointItem | ErrorItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<BreakpointItem | ErrorItem | undefined | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    constructor(private readonly client: EngineClient, private readonly logger: Logger) {}

    /** Force a refresh of the tree view. */
    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    dispose(): void {
        this._onDidChangeTreeData.dispose();
    }

    getTreeItem(element: BreakpointItem | ErrorItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: BreakpointItem | ErrorItem): Promise<(BreakpointItem | ErrorItem)[]> {
        if (element) {
            return [];
        }
        try {
            const list = await this.client.listBreakpoints();
            return (list.breakpoints || []).map(rule => new BreakpointItem(rule));
        } catch (err: any) {
            const msg = err?.message || String(err);
            this.logger.error('Breakpoints view error', { message: msg });
            return [new ErrorItem(`Connection lost: ${msg}`, 'apix.refreshBreakpoints')];
        }
    }
}

/** A single item in the Breakpoints tree view. */
export class BreakpointItem extends vscode.TreeItem {
    constructor(public readonly rule: BreakpointRule) {
        super(rule.label || rule.urlPattern, vscode.TreeItemCollapsibleState.None);

        this.description = rule.methods.length > 0 ? rule.methods.join(', ') : 'ALL';
        this.tooltip = `Pattern: ${rule.urlPattern}\nEnabled: ${rule.enabled}`;
        this.contextValue = 'breakpoint';

        this.iconPath = rule.enabled
            ? new vscode.ThemeIcon('debug-breakpoint')
            : new vscode.ThemeIcon('debug-breakpoint-disabled');

        this.command = {
            command: 'apix.toggleBreakpoint',
            title: 'Toggle',
            arguments: [rule.id],
        };
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
