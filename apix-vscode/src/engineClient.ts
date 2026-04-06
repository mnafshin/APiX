import * as path from 'path';
import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import {
    BreakpointRule,
    ResumeAction,
    ReplaySpec,
    HistoryQuery,
    StatusResponse,
    HttpTransaction,
    PausedRequest,
    BreakpointList,
    HttpResponse,
    PluginInfo,
} from './types';

const PROTO_PATH = path.resolve(__dirname, '../../proto/apix.proto');

// EngineClient wraps the gRPC stub and exposes typed async methods.
export class EngineClient {
    private client: grpc.Client | null = null;
    private stub: any = null;
    private metadata: grpc.Metadata;

    constructor(
        private readonly host: string,
        private readonly port: number,
        private readonly tlsEnabled: boolean,
        private readonly authToken: string
    ) {
        this.metadata = new grpc.Metadata();
        if (authToken) {
            this.metadata.add('authorization', `Bearer ${authToken}`);
        }

        try {
            const packageDef = protoLoader.loadSync(PROTO_PATH, {
                keepCase: false,
                longs: Number,
                enums: String,
                defaults: true,
                oneofs: true,
            });
            const grpcObj = grpc.loadPackageDefinition(packageDef) as any;
            const EngineService = grpcObj.apix.Engine;
            const address = `${host}:${port}`;

            let credentials: grpc.ChannelCredentials;
            if (tlsEnabled) {
                const sslCreds = grpc.credentials.createSsl();
                if (authToken) {
                    const callCreds = grpc.credentials.createFromMetadataGenerator((_, cb) => {
                        const meta = new grpc.Metadata();
                        meta.add('authorization', `Bearer ${authToken}`);
                        cb(null, meta);
                    });
                    credentials = grpc.credentials.combineChannelCredentials(sslCreds, callCreds);
                } else {
                    credentials = sslCreds;
                }
            } else {
                credentials = grpc.credentials.createInsecure();
            }

            this.stub = new EngineService(address, credentials);
            this.client = this.stub as grpc.Client;
        } catch (err) {
            console.error('EngineClient: failed to load proto or create stub:', err);
        }
    }

    private ensureStub(): void {
        if (!this.stub) {
            throw new Error('gRPC stub not initialized. Check proto path and engine connection.');
        }
    }

    /** Check engine health. */
    async getStatus(): Promise<StatusResponse> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            this.stub.getStatus({}, this.metadata, (err: grpc.ServiceError | null, response: any) => {
                if (err) { reject(err); } else { resolve(response as StatusResponse); }
            });
        });
    }

    /** Register a URL breakpoint. */
    async setBreakpoint(rule: BreakpointRule): Promise<BreakpointRule> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            const req = {
                id: rule.id || '',
                urlPattern: rule.urlPattern,
                methods: rule.methods,
                enabled: rule.enabled,
                label: rule.label || '',
            };
            this.stub.setBreakpoint(req, this.metadata, (err: grpc.ServiceError | null, response: any) => {
                if (err) { reject(err); } else { resolve(response.breakpoint as BreakpointRule); }
            });
        });
    }

    /** Delete a breakpoint by ID. */
    async deleteBreakpoint(id: string): Promise<void> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            this.stub.deleteBreakpoint({ id }, this.metadata, (err: grpc.ServiceError | null) => {
                if (err) { reject(err); } else { resolve(); }
            });
        });
    }

    /** List all registered breakpoints. */
    async listBreakpoints(): Promise<BreakpointList> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            this.stub.listBreakpoints({}, this.metadata, (err: grpc.ServiceError | null, response: any) => {
                if (err) { reject(err); } else { resolve(response as BreakpointList); }
            });
        });
    }

    /** List installed plugins. */
    async listPlugins(): Promise<PluginInfo[]> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            this.stub.listPlugins({}, this.metadata, (err: grpc.ServiceError | null, response: any) => {
                if (err) { reject(err); } else { resolve((response.plugins || []) as PluginInfo[]); }
            });
        });
    }

    /**
     * Open a server-streaming RPC that yields newly paused requests.
     * The caller provides callbacks for data, error, and stream end.
     */
    watchPausedRequests(
        onData: (paused: PausedRequest) => void,
        onError: (err: Error) => void,
        onEnd: () => void
    ): grpc.ClientReadableStream<PausedRequest> {
        this.ensureStub();
        const stream: grpc.ClientReadableStream<any> = this.stub.watchPausedRequests({}, this.metadata);
        stream.on('data', (raw: any) => {
            const paused: PausedRequest = {
                requestId: raw.requestId || '',
                request: raw.request || {},
                breakpointId: raw.breakpointId || '',
                pausedAt: raw.pausedAt || 0,
            };
            onData(paused);
        });
        stream.on('error', (err: Error) => onError(err));
        stream.on('end', () => onEnd());
        return stream as grpc.ClientReadableStream<PausedRequest>;
    }

    /** Resume (or drop) a paused request. */
    async resumeRequest(action: ResumeAction): Promise<void> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            const req: any = {
                requestId: action.requestId,
                action: action.action as string,
            };
            if (action.modifiedRequest) {
                req.modifiedRequest = action.modifiedRequest;
            }
            if (action.modifiedResponse) {
                req.modifiedResponse = action.modifiedResponse;
            }
            this.stub.resumeRequest(req, this.metadata, (err: grpc.ServiceError | null) => {
                if (err) { reject(err); } else { resolve(); }
            });
        });
    }

    /** Replay a stored or synthetic request. */
    async replayRequest(spec: ReplaySpec): Promise<HttpResponse> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            const req: any = {
                overrideHeaders: spec.overrideHeaders || {},
                overrideBody: spec.overrideBody || new Uint8Array(),
                followRedirects: spec.followRedirects,
            };
            if (spec.requestId) {
                req.requestId = spec.requestId;
            } else if (spec.rawRequest) {
                req.rawRequest = spec.rawRequest;
            }
            this.stub.replayRequest(req, this.metadata, (err: grpc.ServiceError | null, response: any) => {
                if (err) { reject(err); } else {
                    const resp: HttpResponse = {
                        statusCode: response.statusCode || 0,
                        statusText: response.statusText || '',
                        headers: response.headers || {},
                        body: response.body || '',
                    };
                    resolve(resp);
                }
            });
        });
    }

    /**
     * Fetch history from the engine as a server-streaming RPC.
     * Results are collected and returned as an array.
     */
    async getHistory(query: HistoryQuery): Promise<HttpTransaction[]> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            const req = {
                limit: query.limit,
                offset: query.offset,
                urlFilter: query.urlFilter || '',
                methodFilter: query.methodFilter || '',
                statusFilter: query.statusFilter || 0,
                sinceMs: query.sinceMs || 0,
            };
            const stream: grpc.ClientReadableStream<any> = this.stub.getHistory(req, this.metadata);
            const results: HttpTransaction[] = [];
            stream.on('data', (raw: any) => {
                const tx: HttpTransaction = {
                    id: raw.id || '',
                    request: raw.request || {},
                    response: raw.response || {},
                    timestamp: raw.timestamp || 0,
                    durationMs: raw.durationMs || 0,
                };
                results.push(tx);
            });
            stream.on('error', (err: Error) => reject(err));
            stream.on('end', () => resolve(results));
        });
    }

    /** Clear all stored history. */
    async clearHistory(): Promise<void> {
        this.ensureStub();
        return new Promise((resolve, reject) => {
            this.stub.clearHistory({}, this.metadata, (err: grpc.ServiceError | null) => {
                if (err) { reject(err); } else { resolve(); }
            });
        });
    }

    /** Start the real-time traffic capture stream (CaptureTraffic RPC). */
    captureTraffic(
        onData: (tx: HttpTransaction) => void,
        onError: (err: Error) => void
    ): grpc.ClientReadableStream<HttpTransaction> {
        this.ensureStub();
        const stream: grpc.ClientReadableStream<any> = this.stub.captureTraffic({}, this.metadata);
        stream.on('data', (raw: any) => {
            // CaptureTraffic streams HttpRequest; wrap into a partial HttpTransaction
            const tx: HttpTransaction = {
                id: raw.id || '',
                request: {
                    id: raw.id || '',
                    method: raw.method || '',
                    url: raw.url || '',
                    headers: raw.headers || {},
                    body: raw.body || '',
                    timestamp: raw.timestamp || 0,
                },
                response: {
                    statusCode: 0,
                    statusText: '',
                    headers: {},
                    body: '',
                },
                timestamp: raw.timestamp || Date.now(),
                durationMs: 0,
            };
            onData(tx);
        });
        stream.on('error', (err: Error) => onError(err));
        return stream as grpc.ClientReadableStream<HttpTransaction>;
    }

    /** Close the gRPC channel. */
    close(): void {
        this.client?.close();
    }
}
