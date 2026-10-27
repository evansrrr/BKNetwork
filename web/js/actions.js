import { dns64ModeStateEl, dns64ModeToggleEl, dnsIpv4InputEl, dnsIpv6InputEl, easyModeStateEl, easyModeToggleEl, settingsDiagnosticsBtn, warpStateEl, warpToggleEl } from './dom.js';
import { dns64ModeState, dnsEditorState, fastStatusRefreshState, forgetDns64Backup, pendingToggleState, recallDns64Backup, rememberDns64Backup, settingsState, store } from './state.js';
import { appendLog, applyOptimisticNetworkState, clearPendingToggle, compareVersions, currentIfName, delay, findLocalIpv6Address, getCurrentVersionText, getDns64Backup, getSelectedAdapter, getStableEasyModeValue, getStableWarpValue, isDns64ModeActive, markPendingToggle, parseDnsServers, pickDns64Resolver, renderStatus, renderTrafficUsage, setAdminBadge, setBusy, setDns64ModeStatus, setDnsStatus, setIpv6CheckStatus, setSettingsOpen, setSettingsStatus, setText, showConfigurationFailure, showConfigurationSuccess, showConfigurationToast, showOperationToast, syncDns64ModeState, syncDnsEditor, syncEasyModeState, syncSettingsControls, updateFreeFlowBadge } from './ui.js';
import { fetchIpv6AddressOnce, fetchLatestReleaseTag, postAction, postActionSilently } from './api.js';

export function stopFastStatusRefresh() {
  if (fastStatusRefreshState.intervalId !== null) {
    window.clearInterval(fastStatusRefreshState.intervalId);
    fastStatusRefreshState.intervalId = null;
  }
  if (fastStatusRefreshState.stopTimerId !== null) {
    window.clearTimeout(fastStatusRefreshState.stopTimerId);
    fastStatusRefreshState.stopTimerId = null;
  }
  fastStatusRefreshState.activeUntil = 0;
}

export function startFastStatusRefresh(durationMs = 15000, intervalMs = 2000) {
  const now = Date.now();
  fastStatusRefreshState.activeUntil = Math.max(fastStatusRefreshState.activeUntil, now + durationMs);

  if (fastStatusRefreshState.intervalId === null) {
    fastStatusRefreshState.intervalId = window.setInterval(() => {
      if (Date.now() >= fastStatusRefreshState.activeUntil) {
        stopFastStatusRefresh();
        return;
      }
      refreshStatus().catch(err => console.error('操作失败:', err));
    }, intervalMs);
  }

  if (fastStatusRefreshState.stopTimerId !== null) {
    window.clearTimeout(fastStatusRefreshState.stopTimerId);
  }
  fastStatusRefreshState.stopTimerId = window.setTimeout(() => {
    if (Date.now() >= fastStatusRefreshState.activeUntil) {
      stopFastStatusRefresh();
    }
  }, durationMs);

  refreshStatus().catch(err => console.error('操作失败:', err));
}

export async function checkForNewVersion() {
  if (!settingsState.loaded) {
    await loadSettings().catch(() => {});
  }
  if (settingsState.disableOnlineChecks) {
    return;
  }

  const currentVersion = getCurrentVersionText();
  if (!currentVersion) {
    return;
  }

  const latestVersion = await fetchLatestReleaseTag();
  if (!latestVersion) {
    return;
  }

  const comparison = compareVersions(latestVersion, currentVersion);
  if (comparison === null || comparison <= 0) {
    return;
  }

  showOperationToast('有新版本可用', `版本号：${latestVersion}`, 'warn', 5000);
}

export function scheduleDeferredStartupTasks() {
  const runDeferred = () => {
    checkIpv6Address().catch(err => console.error('操作失败:', err));
    loadSettings().catch(err => console.error('操作失败:', err));
    refreshTrafficUsage().catch(err => console.error('操作失败:', err));
  };

  if ('requestIdleCallback' in window) {
    window.requestIdleCallback(runDeferred, { timeout: 2500 });
    return;
  }

  window.setTimeout(runDeferred, 0);
}

export async function loadSettings() {
  try {
    setSettingsStatus('正在检查...');
    const res = await fetch('/api/v1/settings');
    const data = await res.json();
    settingsState.autoStart = !!data.autoStart;
    settingsState.silentStart = !!data.silentStart;
    settingsState.warpAutoStart = !!data.warpAutoStart;
    settingsState.warpAppAutoStart = !!data.warpAppAutoStart;
    settingsState.disableOnlineChecks = !!data.disableOnlineChecks;
    settingsState.loaded = true;
    syncSettingsControls();
    if (data.warning) {
      // 配置文件损坏时后端返回默认值 + warning，保存一次即可修复
      setSettingsStatus('已使用默认值，重新保存可修复配置文件');
      appendLog(`读取设置失败：${data.warning}`);
      return;
    }
    setSettingsStatus('已加载');
  } catch (err) {
    setSettingsStatus(`加载失败：${err.message}`);
  }
}

export async function openSettingsPanel() {
  await loadSettings();
  setSettingsOpen(true);
}

export async function saveSettings() {
  if (settingsState.saving) {
    return;
  }
  settingsState.saving = true;
  setSettingsStatus('正在保存...');
  try {
    const res = await fetch('/api/v1/settings', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        autoStart: !!settingsState.autoStart,
        silentStart: !!settingsState.silentStart,
        warpAutoStart: !!settingsState.warpAutoStart,
        warpAppAutoStart: !!settingsState.warpAppAutoStart,
        disableOnlineChecks: !!settingsState.disableOnlineChecks,
      }),
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || 'request failed');
    }
    setSettingsStatus('已保存');
    appendLog('设置已保存');
    if (data.warning) {
      appendLog(`设置已保存，但原有配置文件读取失败：${data.warning}`);
    }
  } catch (err) {
    setSettingsStatus(`保存失败：${err.message}`);
    appendLog(`设置保存失败：${err.message}`);
  } finally {
    settingsState.saving = false;
  }
}

// 导出诊断信息：打包版没有可见日志，用户报障时需要一份可直接附带的自检文件。
export async function exportDiagnostics() {
  if (!settingsDiagnosticsBtn) return;
  settingsDiagnosticsBtn.disabled = true;
  setSettingsStatus('正在收集诊断信息...');
  try {
    const res = await fetch('/api/v1/diagnostics', { cache: 'no-store' });
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    const data = await res.json();
    const stamp = new Date().toISOString().replace(/[:.]/g, '-');
    const filename = `bknetwork-diagnostics-${stamp}.json`;
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
    const saved = await saveBlobWithPicker(blob, filename);
    if (!saved) {
      return;
    }
    setSettingsStatus('诊断信息已导出');
    appendLog('诊断信息已导出');
  } catch (err) {
    setSettingsStatus(`导出失败：${err.message}`);
    appendLog(`诊断信息导出失败：${err.message}`);
  } finally {
    settingsDiagnosticsBtn.disabled = false;
  }
}

// saveBlobWithPicker asks the user where to put the file through the File
// System Access API (WebView2 ships a native save dialog for it) and falls
// back to the classic download flow when the picker is unavailable. Returns
// false only when the user closed the dialog without saving.
async function saveBlobWithPicker(blob, filename) {
  if (typeof window.showSaveFilePicker !== 'function') {
    downloadViaAnchor(blob, filename);
    return true;
  }
  try {
    const handle = await window.showSaveFilePicker({
      suggestedName: filename,
      types: [{ description: 'JSON', accept: { 'application/json': ['.json'] } }],
    });
    const writable = await handle.createWritable();
    await writable.write(blob);
    await writable.close();
    return true;
  } catch (err) {
    if (err && err.name === 'AbortError') {
      return false;
    }
    downloadViaAnchor(blob, filename);
    return true;
  }
}

function downloadViaAnchor(blob, filename) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export async function applyDns64Mode(enabled) {
  if (!dns64ModeToggleEl || !dns64ModeStateEl) {
    return;
  }
  if (dns64ModeState.pending) {
    dns64ModeToggleEl.checked = isDns64ModeActive(getSelectedAdapter(store.latestNetwork));
    return;
  }

  const adapter = getSelectedAdapter(store.latestNetwork);
  if (!adapter) {
    setDns64ModeStatus('请先选择目标网卡');
    return;
  }

  if (enabled && (pendingToggleState.warp || getStableWarpValue())) {
    await applyWarpToggle(false);
  }

  // 关闭时要用“开启那一刻记录的备份”，必须在覆盖内存状态之前取出来
  const recordedBackup = recallDns64Backup(adapter.name);
  const backupIpv6Servers = getDns64Backup(adapter);
  dns64ModeState.backupIpv6Servers = enabled ? backupIpv6Servers : recordedBackup;
  if (enabled && backupIpv6Servers.length > 0) {
    rememberDns64Backup(adapter.name, backupIpv6Servers);
  }
  dns64ModeState.pending = { enabled, adapterName: adapter.name };
  dns64ModeToggleEl.checked = enabled;
  setBusy(true);
  showConfigurationToast('DNS64 免流模式', enabled ? '正在开启 DNS64 免流模式' : '正在关闭 DNS64 免流模式');
  setDns64ModeStatus(enabled ? '切换仅v6并配置DNS64...' : '恢复双栈并还原 DNS...');

  try {
    if (enabled) {
      const resolver = pickDns64Resolver();
      await postActionSilently('/api/v1/switch', { ifName: adapter.name, mode: 'ipv6' });
      await postActionSilently('/api/v1/dns', { ifName: adapter.name, ipv6Servers: [resolver] });
      setDns64ModeStatus('当前：DNS64 已启用');
    } else {
      const restoreServers = recordedBackup;
      await postActionSilently('/api/v1/switch', { ifName: adapter.name, mode: 'both' });
      if (restoreServers.length > 0) {
        await postActionSilently('/api/v1/dns', { ifName: adapter.name, ipv6Servers: restoreServers });
      } else {
        // 没有备份时宁可保持现状，也不要硬写成某个公共 DNS
        appendLog('未记录原始 IPv6 DNS，关闭 DNS64 时保留当前 DNS 设置');
      }
      forgetDns64Backup(adapter.name);
      setDns64ModeStatus('当前：双栈');
    }
    dns64ModeState.backupIpv6Servers = enabled ? backupIpv6Servers : [];
    if (enabled) {
      checkIpv6Address().catch(err => console.error('操作失败:', err));
    }
    showConfigurationSuccess();
  } catch (err) {
    try {
      if (enabled) {
        await postActionSilently('/api/v1/switch', { ifName: adapter.name, mode: 'both' });
        if (backupIpv6Servers.length > 0) {
          await postActionSilently('/api/v1/dns', { ifName: adapter.name, ipv6Servers: backupIpv6Servers });
        }
      } else {
        await postActionSilently('/api/v1/switch', { ifName: adapter.name, mode: 'ipv6' });
        await postActionSilently('/api/v1/dns', { ifName: adapter.name, ipv6Servers: [pickDns64Resolver()] });
      }
    } catch (err) {
      console.error('DNS64 回滚失败:', err);
    }
    dns64ModeToggleEl.checked = isDns64ModeActive(getSelectedAdapter(store.latestNetwork));
    setDns64ModeStatus(`切换失败：${err.message}`);
    appendLog(`DNS64 免流模式切换失败：${err.message}`);
    showConfigurationFailure(err.message);
    throw err;
  } finally {
    dns64ModeState.pending = null;
    dns64ModeToggleEl.disabled = false;
    setBusy(false);
    // Fetch fresh status after switch operation to get updated adapter state
    try {
      await refreshStatus(true);
    } catch (_) {
      syncDns64ModeState(store.latestNetwork, true);
    }
  }
}

export async function saveDnsEditor() {
  if (dnsEditorState.saving) {
    return;
  }
  const adapter = getSelectedAdapter(store.latestNetwork);
  if (!adapter) {
    setDnsStatus('请先选择目标网卡');
    return;
  }

  const payload = { ifName: adapter.name };
  if (adapter.ipv4Enabled) {
    payload.ipv4Servers = parseDnsServers(dnsIpv4InputEl?.value || '');
  }
  if (adapter.ipv6Enabled) {
    payload.ipv6Servers = parseDnsServers(dnsIpv6InputEl?.value || '');
  }

  if (!adapter.ipv4Enabled && !adapter.ipv6Enabled) {
    setDnsStatus('当前网卡未启用 IPv4/IPv6，无法保存 DNS');
    return;
  }

  dnsEditorState.saving = true;
  setBusy(true);
  syncDnsEditor(store.latestNetwork, true);
  try {
    const res = await fetch('/api/v1/dns', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(payload),
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || 'request failed');
    }
    dnsEditorState.dirty.ipv4 = false;
    dnsEditorState.dirty.ipv6 = false;
    setDnsStatus('DNS 已保存');
    appendLog('DNS 已保存');
    refreshStatus(true).catch(err => console.error('操作失败:', err));
  } catch (err) {
    setDnsStatus(`保存失败：${err.message}`);
    appendLog(`DNS 保存失败：${err.message}`);
    showConfigurationFailure(err.message);
    throw err;
  } finally {
    dnsEditorState.saving = false;
    setBusy(false);
    syncDnsEditor(store.latestNetwork, true);
  }
}

export async function loadNetworkForLocalCheck() {
  if (store.latestNetwork) {
    return store.latestNetwork;
  }
  try {
    const res = await fetch('/api/v1/status', { cache: 'no-store' });
    const data = await res.json();
    return data?.network || null;
  } catch (_) {
    return null;
  }
}

export async function checkIpv6Address() {
  setIpv6CheckStatus('warn', '正在检测 IPv6 地址...');

  const local = findLocalIpv6Address(await loadNetworkForLocalCheck());
  if (local) {
    setIpv6CheckStatus('ok', local);
    return local;
  }

  if (settingsState.disableOnlineChecks) {
    setIpv6CheckStatus('warn', '未发现 IPv6 地址（已关闭联网检查）');
    return null;
  }

  let lastError = null;
  for (let attempt = 1; attempt <= 2; attempt += 1) {
    try {
      const ipv6 = await fetchIpv6AddressOnce();
      setIpv6CheckStatus('ok', ipv6);
      return ipv6;
    } catch (err) {
      lastError = err;
      if (attempt < 2) {
        await delay(800);
      }
    }
  }
  setIpv6CheckStatus('err', '未检测到ipv6地址，不支持免流功能');
  appendLog(`IPv6 检测失败：${lastError?.message || 'unknown error'}`);
  return null;
}

export function startWarpStatusPoll() {
  const poll = async () => {
    try {
      const res = await fetch('/api/v1/warp-status');
      const data = await res.json();
      store.warpConnected = !!data.connected;
      store.warpStatusText_ = data.status || '';
      if (warpToggleEl) {
        warpToggleEl.checked = store.warpConnected;
        setText(warpStateEl, store.warpConnected ? '当前：已开启' : store.warpStatusText_);
      }
      syncEasyModeState(store.latestNetwork);
      updateFreeFlowBadge();
      if (store.warpConnected && store.easyModePendingSuccess) {
        store.easyModePendingSuccess = false;
        showConfigurationSuccess();
      }
    } catch (err) {
      console.error('WARP 状态轮询失败:', err);
    }
    setTimeout(poll, 3000);
  };
  setTimeout(poll, 1000);
}

export function loadTrafficUsage() {
  return new Promise((resolve, reject) => {
    const url = 'http://202.204.48.66:801/eportal/portal/visitor/loadUserFlow';
    const script = document.createElement('script');
    const previousJsonpReturn = window.jsonpReturn;
    let settled = false;

    function cleanup() {
      clearTimeout(timeoutId);
      script.remove();
      if (previousJsonpReturn === undefined) {
        delete window.jsonpReturn;
      } else {
        window.jsonpReturn = previousJsonpReturn;
      }
    }

    const timeoutId = window.setTimeout(() => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      reject(new Error('timeout'));
    }, 5000);

    window.jsonpReturn = (payload) => {
      if (settled) {
        return;
      }
      settled = true;
      try {
        if (!payload || payload.result !== 1 || !payload.data) {
          throw new Error('invalid payload');
        }
        cleanup();
        resolve(payload);
      } catch (err) {
        cleanup();
        reject(err);
      }
    };

    script.onerror = () => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      reject(new Error('load failed'));
    };

    script.src = `${url}?t=${Date.now()}`;
    document.head.appendChild(script);
  });
}

export function refreshTrafficUsage() {
  return loadTrafficUsage()
    .then((payload) => {
      renderTrafficUsage(payload);
      return payload;
    })
    .catch(err => console.error('操作失败:', err));
}

export async function refreshStatus(force = false) {
  try {
    const res = await fetch('/api/v1/status');
    const data = await res.json();
    renderStatus(data, force);
    appendLog('状态已刷新');
  } catch (err) {
    appendLog(`状态刷新失败：${err.message}`);
  }
}

export async function checkAdminStatus() {
  setAdminBadge('warn', '管理员权限检查中');
  try {
    const res = await fetch('/api/v1/status', { cache: 'no-store' });
    const data = await res.json();
    if (data.adminError) {
      throw new Error(data.adminError);
    }
    const isAdmin = !!data.admin;
    setAdminBadge(isAdmin ? 'ok' : 'warn', isAdmin ? '管理员权限：已启用' : '管理员权限：未启用');
  } catch (err) {
    setAdminBadge('err', '管理员权限：检查失败');
    appendLog(`管理员权限检查失败：${err.message}`);
  }
}

export async function switchStack(mode) {
  const ifName = currentIfName();
  const stackLabel = {
    ipv4: 'IP 栈协议（仅 v4）',
    ipv6: 'IP 栈协议（仅 v6）',
    both: 'IP 栈协议（双栈）',
  }[mode] || 'IP 栈协议';
  showConfigurationToast(stackLabel);
  try {
    await postAction('/api/v1/switch', { ifName, mode });
    store.lastSwitchTime = Date.now();
    applyOptimisticNetworkState({ adapterMode: mode });
    showConfigurationSuccess();
  } catch (err) {
    showConfigurationFailure(err.message);
    throw err;
  }
}

export async function applyWarpToggle(enabled) {
  if (!warpToggleEl || !warpStateEl) {
    return;
  }
  if (pendingToggleState.warp) {
    warpToggleEl.checked = getStableWarpValue();
    return;
  }

  if (enabled && dns64ModeState.pending) {
    warpToggleEl.checked = getStableWarpValue();
    return;
  }

  if (enabled && dns64ModeToggleEl && isDns64ModeActive(getSelectedAdapter(store.latestNetwork))) {
    await applyDns64Mode(false);
  }

  showConfigurationToast('WARP', enabled ? '正在开启 WARP 连接' : '正在关闭 WARP 连接');
  markPendingToggle('warp', enabled, 'warp.ok');
  warpToggleEl.checked = getStableWarpValue();
  setText(warpStateEl, enabled ? '正在开启...' : '正在关闭...');
  setBusy(true);
  let succeeded = false;
  try {
    await postActionSilently('/api/v1/warp', { action: enabled ? 'start' : 'stop' });
    succeeded = true;
    if (enabled) {
      checkIpv6Address().catch(err => console.error('操作失败:', err));
    }
  } catch (err) {
    clearPendingToggle('warp');
    warpToggleEl.checked = getStableWarpValue();
    setText(warpStateEl, '切换失败');
    appendLog(`WARP 切换失败：${err.message}`);
    showConfigurationFailure(err.message);
    throw err;
  } finally {
    setBusy(false);
    if (succeeded) {
      showConfigurationSuccess();
    }
  }
}

export async function applyEasyMode(enabled) {
  if (!easyModeToggleEl || !easyModeStateEl) {
    return;
  }
  if (pendingToggleState.easyMode) {
    easyModeToggleEl.checked = getStableEasyModeValue();
    return;
  }
  showConfigurationToast('免流模式', enabled ? '正在开启Warp免流模式' : '正在关闭Warp免流模式');
  markPendingToggle('easyMode', enabled, enabled ? 'warp.ok' : 'switch.ok');
  easyModeToggleEl.checked = getStableEasyModeValue();
  setText(easyModeStateEl, enabled ? '正在开启...' : '正在关闭...');
  if (enabled) {
    applyOptimisticNetworkState({ adapterMode: 'ipv6' });
  }
  setBusy(true);
  let succeeded = false;
  try {
    if (enabled) {
      await postActionSilently('/api/v1/switch', { ifName: currentIfName(), mode: 'ipv6' });
      await postActionSilently('/api/v1/warp', { action: 'start' });
    } else {
      if (pendingToggleState.warp || getStableWarpValue()) {
        await postActionSilently('/api/v1/warp', { action: 'stop' });
      }
      await postActionSilently('/api/v1/switch', { ifName: currentIfName(), mode: 'both' });
    }
    succeeded = true;
  } catch (err) {
    clearPendingToggle('easyMode');
    easyModeToggleEl.checked = getStableEasyModeValue();
    setText(easyModeStateEl, '切换失败');
    appendLog(`免流模式切换失败：${err.message}`);
    showConfigurationFailure(err.message);
  } finally {
    if (pendingToggleState.easyMode) {
      clearPendingToggle('easyMode');
    }
    setBusy(false);
    // Fetch fresh status after switch operation to get updated adapter state
    try {
      await refreshStatus(true);
    } catch (_) {
      syncEasyModeState(store.latestNetwork);
    }
    if (succeeded && !enabled) {
      showConfigurationSuccess();
    } else if (succeeded && enabled) {
      store.easyModePendingSuccess = true;
      showConfigurationToast('免流模式', '已开启，等待 WARP 连接...');
    }
  }
}
