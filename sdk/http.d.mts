import type { HookContext, HookDescriptor, JSONValue } from './index.mjs';
export type Stage = 'request' | 'resolve' | 'upstream' | 'observe' | 'responseStart' | 'originalBody' | 'responseBody' | 'finish' | 'delivery';
export type Headers = Record<string, string[]>;
/** Detached full snapshot, not a patch. body is canonical base64 when declared. */
export type Message = { method: string; url: string; header: Headers; status: number; body?: string };
export type BufferedMessage = Message & { body: string };
export type Identity = { root: string; exchange: string; parent: string; attempt: number; depth: number };
export type LedgerEntry = { identity: Identity; hook: string; stage: Stage; outcome: string; requestedUrl: string; servedUrl: string; status: number };
export type DeliveryEvidence = { status: number; bytes: number; writeError: string; flushError: string; readError: string; returned: boolean; aborted: boolean; hijacked: boolean };
export type HTTPInput = {
  v: 1; stage: Stage; identity: Identity; requested: Message; request: Message; response: Message;
  metadata: Record<string,string>; ledger: LedgerEntry[]; truncated: boolean; provenance: Identity[];
  auth: { subject: string; claims: Record<string,string> }; error: string;
  delivery?: DeliveryEvidence;
  stream?: { streamId: string; sequence: number; data: string; end: boolean };
};
export type HTTPResult = (
  { action: 'pass' } | { action: 'replace'; message: Message } |
  { action: 'respond'; message: BufferedMessage } | { action: 'child'; request: BufferedMessage }
) & { metadata?: Record<string,string>; credentials?: Headers };
export interface HTTPContext extends HookContext {
  /** Bounded buffered ordinary HTTP to the exact configured gateway origin. No redirects. */
  http(request: BufferedMessage): Promise<BufferedMessage>;
}
export interface HTTPSetupAPI {
  readonly config: JSONValue;
  on(descriptor: HookDescriptor & { stage: 'delivery' }, handler: (input: HTTPInput, context: HookContext) => HTTPResult | Promise<HTTPResult>): void;
  on(descriptor: HookDescriptor & { stage: Exclude<Stage,'delivery'> }, handler: (input: HTTPInput, context: HTTPContext) => HTTPResult | Promise<HTTPResult>): void;
  log(...args: unknown[]): void;
  cleanup(fn: () => void | Promise<void>): void;
}
export type HTTPSetup = (api: HTTPSetupAPI) => void | Promise<void>;
