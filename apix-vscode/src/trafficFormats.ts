import { HttpRequest, HttpTransaction, WebSocketFrame } from './types';

function shellQuote(value: string): string {
    return `'${value.replace(/'/g, `'\"'\"'`)}'`;
}

function bodyAsString(body: Uint8Array | string | undefined): string {
    if (!body) {
        return '';
    }
    if (typeof body === 'string') {
        return body;
    }
    return Buffer.from(body).toString('utf8');
}

function requestFrom(input: HttpRequest | HttpTransaction): HttpRequest {
    return 'request' in input ? input.request : input;
}

export function isWebSocketTransaction(tx: HttpTransaction): boolean {
    const upgrade = tx.request?.headers?.Upgrade || tx.request?.headers?.upgrade || '';
    return upgrade.toLowerCase() === 'websocket' || tx.response?.statusCode === 101;
}

export function bodyToString(body: Uint8Array | string | undefined): string {
    return bodyAsString(body);
}

export function formatWebSocketOpcode(opcode: number): string {
    switch (opcode) {
        case 1: return 'text';
        case 2: return 'binary';
        case 8: return 'close';
        case 9: return 'ping';
        case 10: return 'pong';
        default: return `opcode ${opcode}`;
    }
}

export function formatWebSocketPayload(frame: WebSocketFrame): string {
    if (typeof frame.payload === 'string') {
        return frame.payload;
    }
    return Buffer.from(frame.payload).toString('utf8');
}

export function buildCurlCommand(input: HttpRequest | HttpTransaction): string {
    const request = requestFrom(input);
    const parts: string[] = ['curl'];

    if (request.method && request.method.toUpperCase() !== 'GET') {
        parts.push('-X', request.method.toUpperCase());
    }

    const headerNames = Object.keys(request.headers || {}).sort((a, b) => a.localeCompare(b));
    for (const name of headerNames) {
        if (name.toLowerCase() === 'content-length') {
            continue;
        }
        parts.push('-H', shellQuote(`${name}: ${request.headers[name]}`));
    }

    const body = bodyAsString(request.body);
    if (body !== '') {
        parts.push('--data-raw', shellQuote(body));
    }

    parts.push(shellQuote(request.url));
    return parts.join(' ');
}
