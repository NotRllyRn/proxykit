// Prebuilt dependency-free ESM for the installed HTTP activation bridge.
export default function setup(api) {
  let calls = 0;
  api.on({ id: 'personal-usage', stage: 'observe', bodyMode: 'none' }, (input, { signal }) => {
    if (signal.aborted) throw new Error('cancelled');
    calls++;
    api.log('Personal usage observation', calls);
    return { action: 'pass' };
  });
  api.on({ id: 'usage-header', stage: 'responseStart', bodyMode: 'none' }, input => ({
    action: 'replace',
    message: { ...input.response, header: { ...input.response.header, 'X-Personal-Usage': [String(calls)] } },
  }));
  api.cleanup(() => api.log('Personal usage worker stopped', calls));
}
