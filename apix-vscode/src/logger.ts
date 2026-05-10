import * as vscode from 'vscode';

type LogLevel = 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';

export class Logger {
    constructor(
        private readonly channel: vscode.OutputChannel,
        private readonly scope = 'apix'
    ) {}

    child(scope: string): Logger {
        return new Logger(this.channel, scope);
    }

    debug(message: string, meta?: Record<string, unknown>): void {
        if (!this.isVerboseEnabled()) {
            return;
        }
        this.write('DEBUG', message, meta);
    }

    info(message: string, meta?: Record<string, unknown>): void {
        this.write('INFO', message, meta);
    }

    warn(message: string, meta?: Record<string, unknown>): void {
        this.write('WARN', message, meta);
    }

    error(message: string, meta?: Record<string, unknown>): void {
        this.write('ERROR', message, meta);
    }

    private write(level: LogLevel, message: string, meta?: Record<string, unknown>): void {
        const payload: Record<string, unknown> = {
            ts: new Date().toISOString(),
            level,
            scope: this.scope,
            msg: message,
        };
        if (meta && Object.keys(meta).length > 0) {
            payload.meta = meta;
        }
        this.channel.appendLine(`[APiX] ${JSON.stringify(payload)}`);
    }

    private isVerboseEnabled(): boolean {
        const cfg = vscode.workspace.getConfiguration('apix');
        return cfg.get<boolean>('logging.verbose', false);
    }
}
