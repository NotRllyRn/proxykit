// Embedded trusted worker runtime. stdout is exclusively framed protocol v1.
import { pathToFileURL } from 'node:url';
import { isAbsolute } from 'node:path';
import { format } from 'node:util';
import { Console } from 'node:console';
const wireWrite = process.stdout.write.bind(process.stdout);
const log = (...args) => process.stderr.write(format(...args) + '\n');
const stderrConsole = new Console({ stdout: process.stderr, stderr: process.stderr });
for (const key of Object.keys(console)) if (typeof stderrConsole[key] === 'function') console[key] = stderrConsole[key].bind(stderrConsole);
let maxFrame = 1024 * 1024, concurrency = 16, initialized = false, ready = false, stopping = false;
const hooks = new Map(), active = new Map(), cleanups = [];
const identifier = /^[a-zA-Z][a-zA-Z0-9_.-]{0,127}$/;
let outCount = 0;
function fatal(error) { log('proxykit worker:', error?.stack ?? error); process.exit(1); }
function send(message) {
  const text = JSON.stringify({ v: 1, ...message });
  if (message.type === 'result' && !Object.hasOwn(JSON.parse(text), 'output')) throw new Error('hook must return a JSON value');
  const body = Buffer.from(text);
  if (!body.length || body.length > maxFrame) throw new Error('output frame exceeds bound');
  if (outCount >= concurrency + 8) throw new Error('output queue exceeds bound');
  const frame = Buffer.allocUnsafe(body.length + 4); frame.writeUInt32BE(body.length); body.copy(frame, 4);
  // Validation/allocation rejection is definitely unsent and consumes no queue slot.
  outCount++;
  try { wireWrite(frame, error => { outCount--; if (error) fatal(error); }); }
  // Once a write is attempted, delivery is uncertain: never reuse correlation.
  catch (error) { fatal(error); }
}
function sendHookError(id, code, error) {
  // Bound the complete UTF-8 JSON frame, including escaping and correlation.
  // Keep diagnostic work bounded too; never split a Unicode code point.
  const characters = [];
  for (const character of String(error?.message ?? error) || 'hook failed') {
    characters.push(character);
    if (characters.length === 512) break;
  }
  const message = { type: 'error', id, error: { code, message: '' } };
  let low = 1, high = characters.length;
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    message.error.message = characters.slice(0, middle).join('');
    if (Buffer.byteLength(JSON.stringify({ v: 1, ...message })) <= maxFrame) low = middle;
    else high = middle - 1;
  }
  message.error.message = characters.slice(0, low).join('');
  send(message);
}
function keys(m, required, optional = []) {
  if (!m || Array.isArray(m) || typeof m !== 'object') throw new Error('expected message object');
  if (required.some(k => !Object.hasOwn(m, k)) || Object.keys(m).some(k => !required.includes(k) && !optional.includes(k))) throw new Error('invalid message fields');
}
async function initialize(m) {
  keys(m, ['v', 'type', 'entry', 'config', 'maxFrame', 'concurrency'], ['dataDir']);
  if (m.dataDir !== undefined && (typeof m.dataDir !== 'string' || (m.dataDir !== '' && !isAbsolute(m.dataDir)))) throw new Error('invalid dataDir');
  if (initialized || typeof m.entry !== 'string' || !Number.isInteger(m.maxFrame) || m.maxFrame < 256 || m.maxFrame > 64 * 1024 * 1024 || !Number.isInteger(m.concurrency) || m.concurrency < 1 || m.concurrency > 4096) throw new Error('invalid init');
  initialized = true; maxFrame = m.maxFrame; concurrency = m.concurrency;
  let registrationOpen = true;
  const api = Object.freeze({
    config: m.config,
    dataDir: m.dataDir || undefined,
    log,
    on(descriptor, handler) {
      if (!registrationOpen) throw new Error('registration is closed');
      keys(descriptor, ['id', 'stage', 'bodyMode'], ['order', 'failure', 'formats', 'requires', 'before', 'after']);
      if (descriptor.failure !== undefined && !['block', 'failOpen'].includes(descriptor.failure)) throw new Error('invalid failure policy');
      for (const key of ['formats', 'requires', 'before', 'after']) if (descriptor[key] !== undefined && (!Array.isArray(descriptor[key]) || descriptor[key].some(s => typeof s !== 'string' || !s.length || s.length > 256))) throw new Error('invalid descriptor list');
      if (!identifier.test(descriptor.id) || !identifier.test(descriptor.stage) || !['none', 'stream', 'buffered'].includes(descriptor.bodyMode) || (descriptor.order !== undefined && (typeof descriptor.order !== 'number' || !Number.isFinite(descriptor.order))) || hooks.has(descriptor.id) || hooks.size >= 4096 || typeof handler !== 'function') throw new Error('invalid hook registration');
      hooks.set(descriptor.id, { descriptor: structuredClone(descriptor), handler });
    },
    cleanup(fn) { if (!registrationOpen || typeof fn !== 'function') throw new Error('cleanup must be registered during setup'); cleanups.push(fn); },
  });
  const module = await import(pathToFileURL(m.entry).href);
  if (typeof module.default !== 'function') throw new Error('entry must default-export setup');
  await module.default(api); registrationOpen = false; ready = true;
  send({ type: 'ready', hooks: [...hooks.values()].map(h => h.descriptor) });
}
async function invoke(m) {
  keys(m, ['v', 'type', 'id', 'hook', 'input'], ['deadline', 'managed']);
  if (m.managed !== undefined && typeof m.managed !== 'boolean') throw new Error('invalid managed scope flag');
  if (!ready || stopping || typeof m.id !== 'string' || !/^[1-9][0-9]{0,19}$/.test(m.id) || active.has(m.id) || !hooks.has(m.hook) || active.size >= concurrency || (m.deadline !== undefined && !Number.isSafeInteger(m.deadline))) throw new Error('invalid invocation or capacity exceeded');
  const controller = new AbortController();
  const call = { controller, nextCall: 0, pending: undefined }; active.set(m.id, call);
  controller.signal.addEventListener('abort', () => {
    call.pending?.reject(new Error('cancelled managed scope'));
    call.pending = undefined;
  }, { once: true });
  let timer;
  if (m.deadline !== undefined) {
    const delay = m.deadline - Date.now();
    if (delay <= 0) controller.abort();
    else timer = setTimeout(() => controller.abort(), Math.min(delay, 2147483647));
  }
  try {
    if (controller.signal.aborted) throw new Error('deadline exceeded before invocation');
    const context = { signal: controller.signal, deadline: m.deadline };
    if (m.managed === true && m.input?.v === 1 && m.input?.stage !== 'delivery') context.http = request => {
      if (active.get(m.id) !== call || controller.signal.aborted) return Promise.reject(new Error('expired managed scope'));
      if (call.pending) return Promise.reject(new Error('managed call capacity exhausted'));
      const callId = String(call.nextCall + 1);
      return new Promise((resolve, reject) => {
        call.pending = { callId, resolve, reject };
        try {
          send({ type: 'call', id: m.id, callId, request });
          call.nextCall++;
        }
        catch (error) { call.pending = undefined; reject(error); }
      });
    };
    const output = await hooks.get(m.hook).handler(m.input, Object.freeze(context));
    if (output === undefined) throw new Error('hook must return a JSON value (use null or an explicit pass result)');
    // JSON serialization validation occurs before releasing capacity.
    send({ type: 'result', id: m.id, output });
  } catch (error) {
    sendHookError(m.id, controller.signal.aborted ? 'CANCELLED' : 'HOOK_ERROR', error);
  } finally { clearTimeout(timer); call.pending?.reject(new Error('expired managed scope')); call.pending = undefined; active.delete(m.id); }
}
async function shutdown(m) {
  keys(m, ['v', 'type']); if (!ready || stopping) throw new Error('invalid shutdown'); stopping = true;
  for (const call of active.values()) call.controller.abort();
  // Host enforces a bounded deadline and kills uncooperative work/cleanup.
  while (active.size) await new Promise(resolve => setTimeout(resolve, 5));
  for (const cleanup of cleanups.reverse()) await cleanup();
  send({ type: 'stopped' });
  process.stdin.pause();
  // Ensure stopped is flushed; trusted extension-owned handles are forcibly ended.
  const finish = () => outCount ? setTimeout(finish, 5) : process.exit(0); finish();
}
function message(m) {
  if (m?.v !== 1 || typeof m.type !== 'string') throw new Error('unsupported protocol');
  switch (m.type) {
    case 'init': initialize(m).catch(fatal); break;
    case 'invoke': invoke(m).catch(fatal); break;
    case 'callResult': {
      keys(m, ['v', 'type', 'id', 'callId'], ['output', 'error']);
      if (typeof m.id !== 'string' || typeof m.callId !== 'string' || Object.hasOwn(m, 'output') === Object.hasOwn(m, 'error') || (Object.hasOwn(m, 'error') && typeof m.error !== 'string')) throw new Error('invalid callback result');
      const call = active.get(m.id);
      // A result may already have been queued when the invocation settled.
      if (!call) break;
      if (call.controller.signal.aborted) break;
      if (call.pending?.callId !== m.callId) throw new Error('unmatched callback result');
      const pending = call.pending; call.pending = undefined;
      if (m.error !== undefined) pending.reject(new Error(m.error)); else pending.resolve(m.output);
      break;
    }
    case 'cancel': keys(m, ['v', 'type', 'id']); if (!ready || typeof m.id !== 'string') throw new Error('invalid cancel'); active.get(m.id)?.controller.abort(); break;
    case 'shutdown': shutdown(m).catch(fatal); break;
    default: throw new Error('unknown message type');
  }
}
let header = Buffer.alloc(4), headerUsed = 0, body, bodyUsed = 0;
const decoder = new TextDecoder('utf-8', { fatal: true });
process.stdin.on('data', chunk => {
  try {
    let offset = 0;
    while (offset < chunk.length) {
      if (!body) {
        const n = Math.min(4 - headerUsed, chunk.length - offset); chunk.copy(header, headerUsed, offset, offset + n); headerUsed += n; offset += n;
        if (headerUsed < 4) continue;
        const length = header.readUInt32BE(); if (!length || length > maxFrame) throw new Error('invalid frame length');
        body = Buffer.allocUnsafe(length); bodyUsed = 0; headerUsed = 0;
      }
      const n = Math.min(body.length - bodyUsed, chunk.length - offset); chunk.copy(body, bodyUsed, offset, offset + n); bodyUsed += n; offset += n;
      if (bodyUsed === body.length) { const text = decoder.decode(body); body = undefined; message(JSON.parse(text)); }
    }
  } catch (error) { fatal(error); }
});
process.stdin.on('end', () => { if (!stopping) fatal(new Error('host disconnected')); });
process.stdin.on('error', fatal);
process.on('uncaughtException', fatal);
process.on('unhandledRejection', fatal);
