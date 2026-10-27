import { store } from './state.js';
import { appendLog, setBusy } from './ui.js';

export async function fetchLatestReleaseTag() {
  try {
    const response = await fetch('/api/v1/version/latest', {
      cache: 'no-store',
    });
    if (!response.ok) {
      return '';
    }
    const data = await response.json();
    return typeof data?.tag === 'string' ? data.tag : '';
  } catch (err) {
    console.error('版本检查失败:', err);
    return '';
  }
}

export async function fetchIpv6AddressOnce() {
  const controller = new AbortController();
  const timeoutId = window.setTimeout(() => controller.abort(), 5000);
  try {
    const res = await fetch('https://api-ipv6.ip.sb/ip', {
      method: 'GET',
      cache: 'no-store',
      signal: controller.signal,
    });
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    const text = (await res.text()).trim();
    if (!text) {
      throw new Error('empty response');
    }
    return text;
  } finally {
    clearTimeout(timeoutId);
  }
}

// 本地快照里已有的全局 IPv6 地址足以判断免流是否可用，不必再问公网回显服务。

export async function postAction(path, body) {
  setBusy(true);
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    appendLog(`${path} -> ${data.ok ? 'ok' : 'error'}`);
    if (!res.ok) {
      throw new Error(data.error || 'request failed');
    }
    return data;
  } catch (err) {
    appendLog(`请求失败：${err.message}`);
    throw err;
  } finally {
    setBusy(false);
  }
}

export async function postActionSilently(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await res.json();
  appendLog(`${path} -> ${data.ok ? 'ok' : 'error'}`);
  if (!res.ok) {
    throw new Error(data.error || 'request failed');
  }
  return data;
}
