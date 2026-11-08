// Prebuilt dependency-free ESM. Stage labels are interpreted by the future host bridge.
export default function setup(api) {
  let calls = 0;
  api.on({ id: 'personal-usage', stage: 'observe', bodyMode: 'none' }, (input, { signal }) => {
    if (signal.aborted) throw new Error('cancelled');
    calls++;
    api.log('Personal usage observation', calls);
    return { action: 'pass' };
  });
  api.cleanup(() => api.log('Personal usage worker stopped', calls));
}
