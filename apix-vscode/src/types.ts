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
    requestId: string;
    request: HttpRequest;
    response: HttpResponse;
    timestamp: number; // Unix ms
    durationMs: number;
    graphql?: GraphQLMetadata;
}

export interface GraphQLRequestMetadata {
    operationName: string;
    query: string;
    variablesJson: string;
    isBatch: boolean;
    operationCount: number;
}

export interface GraphQLError {
    message: string;
    pathJson: string;
    locationsJson: string;
    extensionsJson: string;
    rawJson: string;
}

export interface GraphQLResponseMetadata {
    errors: GraphQLError[];
}

export interface GraphQLMetadata {
    request?: GraphQLRequestMetadata;
    response?: GraphQLResponseMetadata;
}

export interface WebSocketFrame {
    transactionId: string;
    direction: 'client' | 'server' | string;
    opcode: number;
    payload: Uint8Array | string;
    timestampMs: number;
}

export interface BreakpointRule {
    id: string;        // empty when creating; server assigns UUID
    urlPattern: string;
    methods: string[]; // empty = all methods
    enabled: boolean;
    label?: string;
    headerName?: string;
    headerValue?: string;
    bodyPattern?: string;
    statusCodes?: number[];
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

export interface RequestTemplate {
    id: string;
    name: string;
    request: HttpRequest;
    updatedAt?: number;
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

export interface MatchCriteria {
    urlPattern: string;
    method: string;
    headerName: string;
    headerValue: string;
    bodyPattern: string;
    statusCode: number;
}

export interface RewriteRule {
    id: string;
    name: string;
    enabled: boolean;
    priority: number;
    match?: MatchCriteria;
    action: string;       // RewriteAction enum string e.g. "ADD_REQUEST_HEADER"
    paramKey: string;
    paramValue: string;
    bodyTemplate: Uint8Array | string;
    responseStatus: number;
    responseBody: Uint8Array | string;
    responseContentType: string;
}

export interface RewriteRuleList {
    rules: RewriteRule[];
}
