import * as vscode from 'vscode';
import { EngineClient } from './engineClient';
import { BreakpointRule } from './types';

/**
 * BreakpointsProvider implements TreeDataProvider for the APiX Breakpoints view.
 * Each item represents one BreakpointRule registered in the engine.
 */
export class BreakpointsProvider implements vscode.TreeDataProvider<BreakpointItem> {
    private _onDidChangeTreeData = new vscode.EventEmitter<BreakpointItem | undefined | void>();
    readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

    constructor(private readonly client: EngineClient) {}

    /** Force a refresh of the tree view. */
    refresh(): void {
        this._onDidChangeTreeData.fire();
    }

    dispose(): void {
        this._onDidChangeTreeData.dispose();
    }

    getTreeItem(element: BreakpointItem): vscode.TreeItem {
        return element;
    }

    async getChildren(element?: BreakpointItem): Promise<BreakpointItem[]> {
        if (element) {
            return [];
        }
        try {
            const list = await this.client.listBreakpoints();
            return (list.breakpoints || []).map(rule => new BreakpointItem(rule));
        } catch {
            return [];
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
