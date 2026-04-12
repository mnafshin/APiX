import { HttpRequest, HttpTransaction } from './types';

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
