import * as vscode from 'vscode';
import * as child_process from 'child_process';
import * as path from 'path';

/**
 * EngineProcessManager starts and stops the apix-engine binary as a child
 * process of the VS Code extension host.
 *
 * In remote/browser mode (vscode.dev) this manager is not used; the extension
 * connects directly to a user-supplied remote engine address instead.
 */
// Auto-restart configuration constants.
const BACKOFF_INITIAL_MS = 1_000;
const BACKOFF_MAX_MS = 30_000;
const MAX_RESTART_ATTEMPTS = 5;

export class EngineProcessManager {
    private process: child_process.ChildProcess | null = null;
    private isStarting = false;
    private startPromise: Promise<void> | null = null;
    private restartAttempts = 0;
    private restartTimer: ReturnType<typeof setTimeout> | null = null;
    /** Callback invoked after a successful auto-restart so streams can be re-established. */
    onRestart: (() => void) | null = null;

    constructor(private readonly context: vscode.ExtensionContext) {}

    /** Start the engine subprocess. Resolves when the engine signals readiness. */
    async start(): Promise<void> {
        // Idempotent: if already running, return immediately
        if (this.isRunning()) { return; }
        
        // If already starting, return the existing promise to prevent race condition
        if (this.isStarting && this.startPromise) { 
            return this.startPromise;
        }

        this.isStarting = true;
        try {
            const binaryPath = this._binaryPath();
            const outputChannel = vscode.window.createOutputChannel('APiX Engine');
            outputChannel.show(true);

            this.startPromise = new Promise((resolve, reject) => {
                try {
                    const proc = child_process.spawn(binaryPath, [], {
                        stdio: ['ignore', 'pipe', 'pipe'],
                    });
                    this.process = proc;

                    let resolved = false;
                    const resolveOnce = () => {
                        if (!resolved) {
                            resolved = true;
                            this.isStarting = false;
                            this.startPromise = null;
                            resolve();
                        }
                    };

                    const timeout = setTimeout(() => {
                        outputChannel.appendLine('[APiX] Warning: engine did not signal ready within 10s — proceeding anyway.');
                        resolveOnce();
                    }, 10000);

                    proc.stdout?.on('data', (data: Buffer) => {
                        const text = data.toString();
                        outputChannel.append(text);
                        if (text.includes('gRPC server listening') || text.includes('Starting gRPC')) {
                            clearTimeout(timeout);
                            resolveOnce();
                        }
                    });

                    proc.stderr?.on('data', (data: Buffer) => {
                        outputChannel.append(data.toString());
                    });

                    proc.on('error', (err: Error) => {
                        clearTimeout(timeout);
                        outputChannel.appendLine(`[APiX] Process error: ${err.message}`);
                        if (!resolved) {
                            resolved = true;
                            this.isStarting = false;
                            this.startPromise = null;
                            reject(err);
                        }
                    });

                    proc.on('exit', (code: number | null, signal: string | null) => {
                        clearTimeout(timeout);
                        if (this.process === proc) {
                            this.process = null;
                        }
                        if (code !== 0 && code !== null) {
                            const msg = `APiX Engine exited unexpectedly (code ${code}, signal ${signal})`;
                            outputChannel.appendLine(`[APiX] ${msg}`);
                            vscode.window.showErrorMessage(msg, 'Restart').then(choice => {
                                if (choice === 'Restart') { 
                                    // Don't await — let user see error message first
                                    void this.start();
                                }
                            });
                        }
                    });
                } catch (err) {
                    this.isStarting = false;
                    this.startPromise = null;
                    reject(err);
                }
            });
            
            await this.startPromise;
        } finally {
            // Ensure cleanup (though resolveOnce should have already done this)
            if (this.startPromise === null) {
                this.isStarting = false;
            }
        }
    }

    /** Stop the engine subprocess gracefully (SIGTERM → wait 3s → SIGKILL). */
    stop(): void {
        if (!this.process) { return; }
        const proc = this.process;
        this.process = null;
        proc.kill('SIGTERM');
        const killTimer = setTimeout(() => {
            if (!proc.killed) { proc.kill('SIGKILL'); }
        }, 3000);
        proc.once('exit', () => clearTimeout(killTimer));
    }

    /** Returns true if the engine process is currently running. */
    isRunning(): boolean {
        return this.process !== null && !this.process.killed;
    }

    /** Resolve the path to the bundled engine binary. */
    private _binaryPath(): string {
        const config = vscode.workspace.getConfiguration('apix');
        const override: string = config.get('engine.binaryPath', '');
        if (override) { return override; }

        // In development: prefer a binary at the workspace root (repo root).
        const workspaceFolders = vscode.workspace.workspaceFolders;
        if (workspaceFolders && workspaceFolders.length > 0) {
            const wsRoot = workspaceFolders[0].uri.fsPath;
            const wsBinary = path.join(wsRoot, 'apix-engine');
            try {
                require('fs').accessSync(wsBinary, require('fs').constants.X_OK);
                return wsBinary;
            } catch {
                // Not found or not executable — fall through.
            }
        }

        // Production: use the binary bundled inside the extension's bin/ dir.
        return path.join(this.context.extensionPath, 'bin', 'apix-engine');
    }
}
