export type JSONValue = null | boolean | number | string | JSONValue[] | { [key: string]: JSONValue };
export interface HookDescriptor { id: string; stage: string; bodyMode: 'none' | 'stream' | 'buffered'; order?: number }
export interface HookContext { signal: AbortSignal; deadline?: number }
export interface SetupAPI {
  readonly config: JSONValue;
  on(descriptor: HookDescriptor, handler: (input: JSONValue, context: HookContext) => JSONValue | Promise<JSONValue>): void;
  log(...args: unknown[]): void;
  cleanup(fn: () => void | Promise<void>): void;
}
export type Setup = (api: SetupAPI) => void | Promise<void>;
export declare function defineExtension(setup: Setup): Setup;
export declare function pass(): { action: 'pass' };
export type StreamUnit = { streamId: string; sequence: number; data: string; end: boolean };
export declare function streamUnit(streamId: string, sequence: number, data: Uint8Array, end?: boolean, maxBytes?: number): StreamUnit;
export declare function streamBytes(unit: StreamUnit, maxBytes?: number): Uint8Array;
