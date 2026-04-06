// Shared TypeScript types mirroring the proto-defined message shapes.
// These are plain interfaces (not proto-generated classes) so they work
// in both the extension host and webview contexts.

export interface HttpRequest {
    id: string;
    method: string;
    url: string;
    headers: Record<string, string>;
    body: Uint8Array | string;
    timestamp: number; // Unix ms
}

export interface HttpResponse {
    statusCode: number;
    statusText: string;
    headers: Record<string, string>;
    body: Uint8Array | string;
}

export interface HttpTransaction {
    id: string;
    request: HttpRequest;
    response: HttpResponse;
    timestamp: number; // Unix ms
    durationMs: number;
}

export interface BreakpointRule {
    id: string;        // empty when creating; server assigns UUID
    urlPattern: string;
    methods: string[]; // empty = all methods
    enabled: boolean;
    label?: string;
}

export interface BreakpointList {
    breakpoints: BreakpointRule[];
}

export interface BreakpointID {
    id: string;
}

export interface PausedRequest {
    requestId: string;
    request: HttpRequest;
    breakpointId: string;
    pausedAt: number; // Unix ms
}

/** Action to take when resuming a paused request. */
export const enum ResumeActionKind {
    Forward  = 'FORWARD',
    Drop     = 'DROP',
    Respond  = 'RESPOND',
}

export interface ResumeAction {
    requestId: string;
    action: ResumeActionKind;
    modifiedRequest?: HttpRequest;    // only when action = Forward
    modifiedResponse?: HttpResponse;  // only when action = Respond
}

export interface ReplaySpec {
    requestId?: string;       // replay by history ID
    rawRequest?: HttpRequest;  // replay arbitrary request
    overrideHeaders?: Record<string, string>;
    overrideBody?: Uint8Array | string;
    followRedirects: boolean;
}

export interface HistoryQuery {
    limit: number;
    offset: number;
    urlFilter?: string;
    methodFilter?: string;
    statusFilter?: number; // 0 = no filter
    sinceMs?: number;
}

export interface StatusResponse {
    status: string;
    version: string;
    proxyPort: number;
    grpcPort: number;
    tlsEnabled: boolean;
}

export interface PluginInfo {
    name: string;
    version: string;
    description: string;
    enabled: boolean;
}
