export function API(getToken) {
  async function request(path, opts = {}) {
    const res = await fetch(path, {
      method: opts.method || 'GET',
      headers: {
        Authorization: 'Bearer ' + getToken(),
        ...(opts.body ? { 'Content-Type': 'application/json' } : {}),
      },
      body: opts.body ? JSON.stringify(opts.body) : undefined,
    });
    if (res.status === 401) {
      window.dispatchEvent(new Event('sw-unauthorized'));
      throw new Error('unauthorized');
    }
    if (res.status === 204) return null;
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error || 'Request failed');
    return data;
  }
  async function blob(path) {
    const res = await fetch(path, {
      headers: { Authorization: 'Bearer ' + getToken() },
    });
    if (res.status === 401) {
      window.dispatchEvent(new Event('sw-unauthorized'));
      throw new Error('unauthorized');
    }
    if (!res.ok) throw new Error('missing');
    return res.blob();
  }
  return {
    get: (path) => request(path),
    blob: (path) => blob(path),
    post: (path, body) => request(path, { method: 'POST', body }),
    patch: (path, body) => request(path, { method: 'PATCH', body }),
    put: (path, body) => request(path, { method: 'PUT', body }),
    del: (path, body) => request(path, { method: 'DELETE', body }),
  };
}
