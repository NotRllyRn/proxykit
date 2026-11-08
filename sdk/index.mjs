// Dependency-free authoring helpers. The runtime supplies the setup API.
export const pass = () => ({ action: 'pass' });
export function defineExtension(setup) {
  if (typeof setup !== 'function') throw new TypeError('setup must be a function');
  return setup;
}
// Typed HTTP authoring uses the same dependency-free runtime registration API.
export const defineHTTP = defineExtension;
export const replace = message => ({ action: 'replace', message });
export const respond = message => ({ action: 'respond', message });
export const child = request => ({ action: 'child', request });
// Limits refer to decoded bytes. The base64 JSON envelope must also fit MaxFrame.
export function streamUnit(streamId, sequence, data, end = false, maxBytes = 64 * 1024) {
  if (typeof streamId !== 'string' || !streamId.length || streamId.length > 128 || !Number.isSafeInteger(sequence) || sequence < 0 || !(data instanceof Uint8Array) || !Number.isSafeInteger(maxBytes) || maxBytes < 1 || data.byteLength > maxBytes || typeof end !== 'boolean') throw new TypeError('invalid bounded stream unit');
  return { streamId, sequence, data: Buffer.from(data).toString('base64'), end };
}
export function streamBytes(unit, maxBytes = 64 * 1024) {
  if (!Number.isSafeInteger(maxBytes) || maxBytes < 1 || !unit || typeof unit.data !== 'string' || unit.data.length > Math.ceil(maxBytes / 3) * 4 || !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(unit.data)) throw new TypeError('invalid base64 stream data');
  const bytes = Buffer.from(unit.data, 'base64');
  const normalized = streamUnit(unit.streamId, unit.sequence, bytes, unit.end, maxBytes);
  if (normalized.data !== unit.data) throw new TypeError('noncanonical base64 stream data');
  return bytes;
}
