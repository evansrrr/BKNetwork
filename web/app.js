const logEl = document.getElementById('log');
const advancedModeToggleEl = document.getElementById('advancedModeToggle');
const backendBadgeEl = document.getElementById('backendBadge');
const backendBadgeDotEl = document.getElementById('backendDot');
const adminBadgeEl = document.getElementById('adminBadge');
const adminBadgeDotEl = document.getElementById('adminDot');
const networkBadgeEl = document.getElementById('networkBadge');
const networkBadgeDotEl = document.getElementById('networkDot');
const freeBadgeEl = document.getElementById('freeBadge');
const freeBadgeDotEl = document.getElementById('freeDot');
const ipv6CheckDotEl = document.getElementById('ipv6CheckDot');
const ipv6CheckTextEl = document.getElementById('ipv6CheckText');
const ipv6RefreshBtnEl = document.getElementById('ipv6RefreshBtn');
const warpToggleEl = document.getElementById('warpToggle');
const warpStateEl = document.getElementById('warpState');
const warpSettingsStateEl = document.getElementById('warpSettingsState');
const warpSettingsModeEl = document.getElementById('warpSettingsMode');
const warpSettingsTunnelProtocolEl = document.getElementById('warpSettingsTunnelProtocol');
const easyModeToggleEl = document.getElementById('easyModeToggle');
const easyModeStateEl = document.getElementById('easyModeState');
const dns64ModeToggleEl = document.getElementById('dns64ModeToggle');
const dns64ModeStateEl = document.getElementById('dns64ModeState');
const settingsOpenBtn = document.getElementById('settingsOpenBtn');
const settingsOverlayEl = document.getElementById('settingsOverlay');
const settingsCloseBtn = document.getElementById('settingsCloseBtn');
const settingAutoStartEl = document.getElementById('settingAutoStart');
const settingSilentStartEl = document.getElementById('settingSilentStart');
const settingWarpAutoStartEl = document.getElementById('settingWarpAutoStart');
const settingWarpAppAutoStartEl = document.getElementById('settingWarpAppAutoStart');
const operationToastEl = document.getElementById('operationToast');
const operationToastTitleEl = document.getElementById('operationToastTitle');
const operationToastDescEl = document.getElementById('operationToastDesc');
const settingsStatusEl = document.getElementById('settingsStatus');
const stackModeStateEl = document.getElementById('stackModeState');
const dnsCardEl = document.getElementById('dnsCard');
const appVersionEl = document.querySelector('.lead');
const dnsIpv4InputEl = document.getElementById('dnsIpv4Input');
const dnsIpv6InputEl = document.getElementById('dnsIpv6Input');
const dnsStatusEl = document.getElementById('dnsStatus');
const lastResultEl = document.getElementById('lastResult');
const lastUpdatedEl = document.getElementById('lastUpdated');
const trafficUsageEl = document.getElementById('trafficUsage');
const adapterListEl = document.getElementById('adapterList');
const targetAdapterSelects = ['ifName', 'ifName2'].map(id => document.getElementById(id));
const stackModeButtons = {
  ipv4: document.getElementById('btnIpv4'),
  ipv6: document.getElementById('btnIpv6'),
  both: document.getElementById('btnBoth'),
};
const buttons = [stackModeButtons.ipv4, stackModeButtons.ipv6, stackModeButtons.both, warpToggleEl, easyModeToggleEl, dns64ModeToggleEl].filter(Boolean);
let latestNetwork = null;
const badgeState = {
  network: { stable: null, pending: null },
  warp: { stable: null, pending: null },
};
const settingsState = {
  autoStart: false,
  silentStart: false,
  warpAutoStart: false,
  warpAppAutoStart: false,
  saving: false,
};
const operationToastState = {
  hideTimer: null,
  initializationDone: false,
  versionCheckStarted: false,
};
const fastStatusRefreshState = {
  intervalId: null,
  stopTimerId: null,
  activeUntil: 0,
};
const pendingToggleState = {
  warp: null,
  easyMode: null,
};
const dns64ModeState = {
  pending: null,
  backupIpv6Servers: [],
};
const dnsEditorState = {
  adapterName: '',
  committed: {
    ipv4: '',
    ipv6: '',
  },
  dirty: {
    ipv4: false,
    ipv6: false,
  },
  saving: false,
};

function setTargetAdapter(value) {
  const next = typeof value === 'string' && value.trim() !== '' ? value.trim() : 'WiFi';
  for (const select of targetAdapterSelects) {
    if (select) {
      select.value = next;
    }
  }
  syncEasyModeState(latestNetwork);
  syncDnsEditor(latestNetwork);
  syncDns64ModeState(latestNetwork);
}

for (const select of targetAdapterSelects) {
  if (!select) continue;
  select.addEventListener('change', () => setTargetAdapter(select.value));
}

function setBusy(busy) {
  buttons.forEach(btn => btn.disabled = busy);
}

function stopFastStatusRefresh() {
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

function startFastStatusRefresh(durationMs = 15000, intervalMs = 2000) {
  const now = Date.now();
  fastStatusRefreshState.activeUntil = Math.max(fastStatusRefreshState.activeUntil, now + durationMs);

  if (fastStatusRefreshState.intervalId === null) {
    fastStatusRefreshState.intervalId = window.setInterval(() => {
      if (Date.now() >= fastStatusRefreshState.activeUntil) {
        stopFastStatusRefresh();
        return;
      }
      refreshStatus().catch(() => {});
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

  refreshStatus().catch(() => {});
}

function getStableWarpValue() {
  return warpConnected;
}

function getStableEasyModeValue() {
  const ifName = currentIfName();
  const adapter = Array.isArray(latestNetwork?.adapters)
    ? latestNetwork.adapters.find(item => item && item.name === ifName)
    : null;
  return !!latestNetwork?.warp?.connected && !!adapter?.freeFlow;
}

function clearPendingToggle(kind) {
  pendingToggleState[kind] = null;
  if (!pendingToggleState.warp && !pendingToggleState.easyMode) {
    stopFastStatusRefresh();
  }
}

function markPendingToggle(kind, enabled, finishOnEvent) {
  pendingToggleState[kind] = { enabled, finishOnEvent };
  startFastStatusRefresh();
}

function settleBoolState(state, next, force = false) {
  if (force || state.stable === null) {
    state.stable = next;
    state.pending = null;
    return next;
  }
  if (state.stable === next) {
    state.pending = null;
    return next;
  }
  if (state.pending === next) {
    state.stable = next;
    state.pending = null;
    return next;
  }
  state.pending = next;
  return state.stable;
}

function applyOptimisticNetworkState({ warpConnected, adapterMode } = {}) {
  if (!latestNetwork) {
    return;
  }

  const targetName = currentIfName();
  const nextAdapters = Array.isArray(latestNetwork.adapters)
    ? latestNetwork.adapters.map(adapter => {
        if (!adapter || adapter.name !== targetName) {
          return adapter;
        }
        const nextAdapter = { ...adapter };
        if (adapterMode === 'ipv4') {
          nextAdapter.ipv4Enabled = true;
          nextAdapter.ipv6Enabled = false;
        } else if (adapterMode === 'ipv6') {
          nextAdapter.ipv4Enabled = false;
          nextAdapter.ipv6Enabled = true;
        } else if (adapterMode === 'both') {
          nextAdapter.ipv4Enabled = true;
          nextAdapter.ipv6Enabled = true;
        }
        if (typeof warpConnected === 'boolean') {
          nextAdapter.freeFlow = warpConnected && !!nextAdapter.ipv6Enabled && !nextAdapter.ipv4Enabled;
        }
        return nextAdapter;
      })
    : latestNetwork.adapters;

  latestNetwork = {
    ...latestNetwork,
    warp: typeof warpConnected === 'boolean'
      ? { ...(latestNetwork.warp || {}), connected: warpConnected }
      : latestNetwork.warp,
    adapters: nextAdapters,
  };
  renderStatus({
    service: { name: 'BKNetwork' },
    lastEvent: { type: 'network.status', message: 'optimistic update' },
    network: latestNetwork,
  }, true);
}

function getAdapterMode(adapter) {
  const ipv4 = !!adapter?.ipv4Enabled;
  const ipv6 = !!adapter?.ipv6Enabled;
  if (ipv4 && !ipv6) {
    return 'ipv4';
  }
  if (!ipv4 && ipv6) {
    return 'ipv6';
  }
  if (ipv4 && ipv6) {
    return 'both';
  }
  return 'unknown';
}

function setAdvancedMode(enabled) {
  const advancedOnly = document.querySelector('.advanced-only');
  const easyMode = document.querySelector('.easy-mode');
  const duration = 200;

  function fadeIn(el) {
    el.style.display = 'block';
    el.style.opacity = '0';
    el.style.transition = 'none';
    void el.offsetHeight;
    el.style.transition = `opacity ${duration}ms ease`;
    el.style.opacity = '1';
  }

  function fadeOut(el, cb) {
    el.style.transition = `opacity ${duration}ms ease`;
    el.style.opacity = '0';
    setTimeout(() => {
      el.style.display = 'none';
      el.style.opacity = '';
      el.style.transition = '';
      if (cb) cb();
    }, duration);
  }

  if (enabled) {
    if (easyMode) fadeOut(easyMode);
    if (advancedOnly) fadeIn(advancedOnly);
  } else {
    if (advancedOnly) fadeOut(advancedOnly);
    if (easyMode) fadeIn(easyMode);
  }

  document.body.classList.toggle('advanced-mode', !!enabled);
  if (advancedModeToggleEl) {
    advancedModeToggleEl.checked = !!enabled;
  }
}

function setDot(dotEl, tone) {
  if (dotEl) dotEl.className = `dot ${tone || ''}`.trim();
}

function setText(el, value) {
  if (el) {
    el.textContent = value;
  }
}

function setSettingsStatus(value) {
  setText(settingsStatusEl, value);
}

function setDnsStatus(value) {
  setText(dnsStatusEl, value);
}

function setDns64ModeStatus(value) {
  setText(dns64ModeStateEl, value);
}

function clearOperationToastTimer() {
  if (operationToastState.hideTimer !== null) {
    clearTimeout(operationToastState.hideTimer);
    operationToastState.hideTimer = null;
  }
}

function showOperationToast(title, desc = '', tone = 'info', autoHideMs = 0) {
  if (!operationToastEl) {
    return;
  }
  clearOperationToastTimer();
  setText(operationToastTitleEl, title);
  setText(operationToastDescEl, desc);
  operationToastEl.dataset.tone = tone;
  operationToastEl.classList.add('visible');
  if (autoHideMs > 0) {
    operationToastState.hideTimer = window.setTimeout(() => {
      operationToastEl.classList.remove('visible');
      operationToastState.hideTimer = null;
    }, autoHideMs);
  }
}

function showInitializationToast() {
  if (operationToastState.initializationDone) {
    return;
  }
  showOperationToast('正在初始化...', '正在连接 WebSocket 并同步页面状态', 'info');
}

function markInitializationComplete() {
  if (operationToastState.initializationDone) {
    return;
  }
  operationToastState.initializationDone = true;
  showOperationToast('初始化成功！', '页面状态已同步完成', 'success', 1800);
  if (!operationToastState.versionCheckStarted) {
    operationToastState.versionCheckStarted = true;
    window.setTimeout(() => {
      checkForNewVersion().catch(() => {});
    }, 0);
  }
}

function showConfigurationToast(label, detail) {
  showOperationToast(`正在配置${label}...`, detail || '', 'info');
}

function showConfigurationSuccess() {
  showOperationToast('配置成功！', '页面状态即将同步更新', 'success', 1600);
}

function showConfigurationFailure(message) {
  showOperationToast('配置失败', message || '请稍后重试', 'warn', 2400);
}

function getCurrentVersionText() {
  return appVersionEl?.textContent?.trim() || '';
}

function parseVersion(version) {
  const match = String(version || '').trim().match(/(?:tag\/)?v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?/);
  if (!match) {
    return null;
  }
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    prerelease: match[4] || '',
  };
}

function compareVersions(leftVersion, rightVersion) {
  const left = parseVersion(leftVersion);
  const right = parseVersion(rightVersion);
  if (!left || !right) {
    return null;
  }
  if (left.major !== right.major) {
    return left.major - right.major;
  }
  if (left.minor !== right.minor) {
    return left.minor - right.minor;
  }
  if (left.patch !== right.patch) {
    return left.patch - right.patch;
  }
  if (left.prerelease === right.prerelease) {
    return 0;
  }
  if (!left.prerelease) {
    return 1;
  }
  if (!right.prerelease) {
    return -1;
  }
  return left.prerelease.localeCompare(right.prerelease, 'en');
}

async function fetchLatestReleaseTag() {
  try {
    const response = await fetch('/api/v1/version/latest', {
      cache: 'no-store',
    });
    if (!response.ok) {
      return '';
    }
    const data = await response.json();
    return typeof data?.tag === 'string' ? data.tag : '';
  } catch {
    return '';
  }
}

async function checkForNewVersion() {
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

function scheduleDeferredStartupTasks() {
  const runDeferred = () => {
    checkIpv6Address().catch(() => {});
    loadSettings().catch(() => {});
    refreshTrafficUsage().catch(() => {});
  };

  if ('requestIdleCallback' in window) {
    window.requestIdleCallback(runDeferred, { timeout: 2500 });
    return;
  }

  window.setTimeout(runDeferred, 0);
}

function syncSettingsControls() {
  if (settingAutoStartEl) settingAutoStartEl.checked = !!settingsState.autoStart;
  if (settingSilentStartEl) settingSilentStartEl.checked = !!settingsState.silentStart;
  if (settingWarpAutoStartEl) settingWarpAutoStartEl.checked = !!settingsState.warpAutoStart;
  if (settingWarpAppAutoStartEl) settingWarpAppAutoStartEl.checked = !!settingsState.warpAppAutoStart;
}

function setSettingsOpen(open) {
  if (!settingsOverlayEl) return;
  settingsOverlayEl.classList.toggle('open', !!open);
  settingsOverlayEl.setAttribute('aria-hidden', String(!open));
  if (open) {
    syncSettingsControls();
  }
}

async function loadSettings() {
  try {
    setSettingsStatus('正在检查...');
    const res = await fetch('/api/v1/settings');
    const data = await res.json();
    settingsState.autoStart = !!data.autoStart;
    settingsState.silentStart = !!data.silentStart;
    settingsState.warpAutoStart = !!data.warpAutoStart;
    settingsState.warpAppAutoStart = !!data.warpAppAutoStart;
    syncSettingsControls();
    setSettingsStatus('已加载');
  } catch (err) {
    setSettingsStatus(`加载失败：${err.message}`);
  }
}

async function openSettingsPanel() {
  await loadSettings();
  setSettingsOpen(true);
}

async function saveSettings() {
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
      }),
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || 'request failed');
    }
    setSettingsStatus('已保存');
    appendLog('设置已保存');
  } catch (err) {
    setSettingsStatus(`保存失败：${err.message}`);
    appendLog(`设置保存失败：${err.message}`);
  } finally {
    settingsState.saving = false;
  }
}

function appendLog(line) {
  const stamp = new Date().toLocaleTimeString();
  if (logEl) {
    logEl.textContent = `[${stamp}] ${line}\n` + logEl.textContent;
  }
}

function setBackendBadge(tone) {
  setDot(backendBadgeDotEl, tone);
}

function setAdminBadge(tone, text) {
  setDot(adminBadgeDotEl, tone);
  setText(adminBadgeEl, text);
}

function setNetworkBadge(tone) {
  setDot(networkBadgeDotEl, tone);
}

function setFreeBadge(tone) {
  setDot(freeBadgeDotEl, tone);
}

function formatDnsServers(values) {
  return Array.isArray(values) && values.length > 0 ? values.join(', ') : '';
}

function parseDnsServers(value) {
  return String(value || '')
    .split(/[\s,;]+/)
    .map(item => item.trim())
    .filter(Boolean);
}

function splitDnsServers(values) {
  const ipv4 = [];
  const ipv6 = [];
  for (const value of Array.isArray(values) ? values : []) {
    const text = typeof value === 'string' ? value.trim() : '';
    if (!text) {
      continue;
    }
    if (text.includes(':')) {
      ipv6.push(text);
    } else {
      ipv4.push(text);
    }
  }
  return { ipv4, ipv6 };
}

const DNS64_CANDIDATES = [
  '2a00:1098:2b::1',
  '2a01:4ff:f0:9876::1',
  '2a01:4f9:c010:3f02::1',
  '2a00:1098:2c::1',
  '2a01:4f8:c2c:123f::1',
];

function pickDns64Resolver() {
  return DNS64_CANDIDATES[Math.floor(Math.random() * DNS64_CANDIDATES.length)];
}

function isDns64ModeActive(adapter) {
  if (!adapter || !adapter.ipv6Enabled || adapter.ipv4Enabled) {
    return false;
  }
  const { ipv6 } = splitDnsServers(adapter.dns);
  return ipv6.length === 1 && DNS64_CANDIDATES.includes(ipv6[0]);
}

function getDns64Backup(adapter) {
  const { ipv6 } = splitDnsServers(adapter?.dns);
  return ipv6;
}

async function postActionWithRollback(path, body) {
  return postActionSilently(path, body);
}

function getSelectedAdapter(network) {
  const ifName = currentIfName();
  return Array.isArray(network?.adapters)
    ? network.adapters.find(item => item && item.name === ifName)
    : null;
}

function syncDnsEditor(network, force = false) {
  if (!dnsIpv4InputEl || !dnsIpv6InputEl) {
    return;
  }

  const adapter = getSelectedAdapter(network);
  if (!adapter) {
    dnsEditorState.adapterName = '';
    dnsEditorState.committed.ipv4 = '';
    dnsEditorState.committed.ipv6 = '';
    dnsEditorState.dirty.ipv4 = false;
    dnsEditorState.dirty.ipv6 = false;
    dnsIpv4InputEl.value = '';
    dnsIpv6InputEl.value = '';
    dnsIpv4InputEl.disabled = true;
    dnsIpv6InputEl.disabled = true;
    setDnsStatus('请先选择目标网卡');
    return;
  }

  const adapterChanged = dnsEditorState.adapterName !== adapter.name;
  if (adapterChanged) {
    dnsEditorState.adapterName = adapter.name;
    dnsEditorState.dirty.ipv4 = false;
    dnsEditorState.dirty.ipv6 = false;
  }

  const { ipv4, ipv6 } = splitDnsServers(adapter.dns);
  const ipv4Value = formatDnsServers(ipv4);
  const ipv6Value = formatDnsServers(ipv6);
  const ipv4Focused = document.activeElement === dnsIpv4InputEl;
  const ipv6Focused = document.activeElement === dnsIpv6InputEl;

  if (force || adapterChanged || (!ipv4Focused && !dnsEditorState.dirty.ipv4)) {
    dnsIpv4InputEl.value = ipv4Value;
    dnsEditorState.committed.ipv4 = ipv4Value;
    dnsEditorState.dirty.ipv4 = false;
  }

  if (force || adapterChanged || (!ipv6Focused && !dnsEditorState.dirty.ipv6)) {
    dnsIpv6InputEl.value = ipv6Value;
    dnsEditorState.committed.ipv6 = ipv6Value;
    dnsEditorState.dirty.ipv6 = false;
  }

  dnsIpv4InputEl.disabled = dnsEditorState.saving || !adapter.ipv4Enabled;
  dnsIpv6InputEl.disabled = dnsEditorState.saving || !adapter.ipv6Enabled;
  dnsIpv4InputEl.placeholder = adapter.ipv4Enabled ? '例如 114.114.114.114, 8.8.8.8' : '当前网卡未启用 IPv4';
  dnsIpv6InputEl.placeholder = adapter.ipv6Enabled ? '例如 2400:3200::1, 2001:4860:4860::8888' : '当前网卡未启用 IPv6';

  if (dnsEditorState.saving) {
    setDnsStatus('正在保存 DNS...');
    return;
  }
  if (!adapter.ipv4Enabled && !adapter.ipv6Enabled) {
    setDnsStatus('当前网卡未启用 IPv4/IPv6，DNS 仅可查看');
    return;
  }
  if (dnsEditorState.dirty.ipv4 || dnsEditorState.dirty.ipv6) {
    setDnsStatus('DNS 有未保存的修改');
    return;
  }
  setDnsStatus('回车或点击文本框外保存');
}

function syncDns64ModeState(network, force = false) {
  if (!dns64ModeToggleEl || !dns64ModeStateEl) {
    return;
  }

  const adapter = getSelectedAdapter(network);
  if (!adapter) {
    dns64ModeState.pending = null;
    dns64ModeToggleEl.checked = false;
    dns64ModeToggleEl.disabled = true;
    setDns64ModeStatus('请先选择目标网卡');
    return;
  }

  if (dns64ModeState.pending) {
    dns64ModeToggleEl.checked = dns64ModeState.pending.enabled;
    dns64ModeToggleEl.disabled = true;
    setDns64ModeStatus(dns64ModeState.pending.enabled ? '正在开启 DNS64...' : '正在关闭 DNS64...');
    return;
  }

  const active = isDns64ModeActive(adapter);
  dns64ModeToggleEl.checked = active;
  dns64ModeToggleEl.disabled = false;

  if (!adapter.ipv4Enabled && !adapter.ipv6Enabled) {
    setDns64ModeStatus('当前网卡未启用 IPv4/IPv6，DNS64 不可用');
    return;
  }
  if (active) {
    setDns64ModeStatus('当前：DNS64 已启用');
    return;
  }
  if (!adapter.ipv6Enabled) {
    setDns64ModeStatus('当前：IPv6 未启用');
    return;
  }
  if (adapter.ipv4Enabled) {
    setDns64ModeStatus('当前：双栈');
    return;
  }
  setDns64ModeStatus('当前：仅 v6');
}

function isFreeFlowModeActive(adapter) {
  if (dns64ModeState.pending?.enabled) {
    return true;
  }
  if (isDns64ModeActive(adapter)) {
    return true;
  }
  if (!adapter) {
    return false;
  }
  const ipv6Only = !!adapter.ipv6Enabled && !adapter.ipv4Enabled;
  return warpConnected && ipv6Only;
}

async function applyDns64Mode(enabled) {
  if (!dns64ModeToggleEl || !dns64ModeStateEl) {
    return;
  }
  if (dns64ModeState.pending) {
    dns64ModeToggleEl.checked = isDns64ModeActive(getSelectedAdapter(latestNetwork));
    return;
  }

  const adapter = getSelectedAdapter(latestNetwork);
  if (!adapter) {
    setDns64ModeStatus('请先选择目标网卡');
    return;
  }

  if (enabled && (pendingToggleState.warp || getStableWarpValue())) {
    await applyWarpToggle(false);
  }

  const backupIpv6Servers = getDns64Backup(adapter);
  dns64ModeState.backupIpv6Servers = backupIpv6Servers;
  dns64ModeState.pending = { enabled, adapterName: adapter.name };
  dns64ModeToggleEl.checked = enabled;
  setBusy(true);
  showConfigurationToast('DNS64 免流模式', enabled ? '正在开启 DNS64 免流模式' : '正在关闭 DNS64 免流模式');
  setDns64ModeStatus(enabled ? '切换仅v6并配置DNS64...' : '恢复双栈并还原 DNS...');

  try {
    if (enabled) {
      const resolver = pickDns64Resolver();
      await postActionSilently('/api/v1/switch', { ifName: adapter.name, mode: 'ipv6' });
      await postActionWithRollback('/api/v1/dns', { ifName: adapter.name, ipv6Servers: [resolver] });
      setDns64ModeStatus('当前：DNS64 已启用');
    } else {
      await postActionSilently('/api/v1/switch', { ifName: adapter.name, mode: 'both' });
      await postActionWithRollback('/api/v1/dns', { ifName: adapter.name, ipv6Servers: ['2606:4700:4700::1111'] });
      setDns64ModeStatus('当前：双栈');
    }
    dns64ModeState.backupIpv6Servers = enabled ? backupIpv6Servers : [];
    if (enabled) {
      checkIpv6Address().catch(() => {});
    }
    refreshStatus(true).catch(() => {});
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
    } catch {
      // best effort rollback
    }
    dns64ModeToggleEl.checked = isDns64ModeActive(getSelectedAdapter(latestNetwork));
    setDns64ModeStatus(`切换失败：${err.message}`);
    appendLog(`DNS64 免流模式切换失败：${err.message}`);
    showConfigurationFailure(err.message);
    throw err;
  } finally {
    dns64ModeState.pending = null;
    dns64ModeToggleEl.disabled = false;
    setBusy(false);
    syncDns64ModeState(latestNetwork, true);
  }
}

async function saveDnsEditor() {
  if (dnsEditorState.saving) {
    return;
  }
  const adapter = getSelectedAdapter(latestNetwork);
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
  syncDnsEditor(latestNetwork, true);
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
    refreshStatus(true).catch(() => {});
  } catch (err) {
    setDnsStatus(`保存失败：${err.message}`);
    appendLog(`DNS 保存失败：${err.message}`);
    showConfigurationFailure(err.message);
    throw err;
  } finally {
    dnsEditorState.saving = false;
    setBusy(false);
    syncDnsEditor(latestNetwork, true);
  }
}

function setIpv6CheckStatus(tone, text) {
  setDot(ipv6CheckDotEl, tone);
  setText(ipv6CheckTextEl, text);
}

function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function fetchIpv6AddressOnce() {
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

async function checkIpv6Address() {
  setIpv6CheckStatus('warn', '正在检测 IPv6 地址...');
  let lastError = null;
  for (let attempt = 1; attempt <= 3; attempt += 1) {
    try {
      const ipv6 = await fetchIpv6AddressOnce();
      setIpv6CheckStatus('ok', ipv6);
      return ipv6;
    } catch (err) {
      lastError = err;
      if (attempt < 3) {
        await delay(800);
      }
    }
  }
  setIpv6CheckStatus('err', '未检测到ipv6地址，不支持免流功能');
  appendLog(`IPv6 检测失败：${lastError?.message || 'unknown error'}`);
  return null;
}

function syncStackModeState(network) {
  const ifName = currentIfName();
  const adapter = Array.isArray(network?.adapters)
    ? network.adapters.find(item => item && item.name === ifName)
    : null;
  const mode = getAdapterMode(adapter);
  const labels = {
    ipv4: '当前：仅 v4',
    ipv6: '当前：仅 v6',
    both: '当前：双栈',
    unknown: '当前：未知',
  };

  setText(stackModeStateEl, labels[mode] || labels.unknown);

  for (const [key, button] of Object.entries(stackModeButtons)) {
    if (!button) continue;
    const active = key === mode;
    button.classList.toggle('active', active);
    button.setAttribute('aria-pressed', String(active));
  }
}

let warpConnected = false;
let warpStatusText_ = '';
let easyModePendingSuccess = false;

function updateFreeFlowBadge() {
  const ifName = currentIfName();
  const adapter = Array.isArray(latestNetwork?.adapters)
    ? latestNetwork.adapters.find(a => a && a.name === ifName)
    : null;
  setFreeBadge(isFreeFlowModeActive(adapter) ? 'ok' : 'warn');
}

function startWarpStatusPoll() {
  const poll = async () => {
    try {
      const res = await fetch('/api/v1/warp-status');
      const data = await res.json();
      warpConnected = !!data.connected;
      warpStatusText_ = data.status || '';
      if (warpToggleEl) {
        warpToggleEl.checked = warpConnected;
        setText(warpStateEl, warpConnected ? '当前：已开启' : warpStatusText_);
      }
      syncEasyModeState(latestNetwork);
      updateFreeFlowBadge();
      if (warpConnected && easyModePendingSuccess) {
        easyModePendingSuccess = false;
        showConfigurationSuccess();
      }
    } catch {}
    setTimeout(poll, 3000);
  };
  setTimeout(poll, 1000);
}

function syncWarpState() {
  if (!warpToggleEl || !warpStateEl) {
    return;
  }
  if (pendingToggleState.warp) {
    setText(warpStateEl, pendingToggleState.warp.enabled ? '正在开启...' : '正在关闭...');
    return;
  }
  warpToggleEl.checked = warpConnected;
  setText(warpStateEl, warpConnected ? '当前：已开启' : warpStatusText_);
}

function fmtSettingValue(value, fallback = '--') {
  return typeof value === 'string' && value.trim() !== '' ? value.trim() : fallback;
}

function syncWarpSettingsState(network) {
  if (!warpSettingsStateEl || !warpSettingsModeEl || !warpSettingsTunnelProtocolEl) {
    return;
  }
  const settings = network?.warpSettings;
  if (!settings) {
    setText(warpSettingsStateEl, '正在加载');
    setText(warpSettingsModeEl, '--');
    setText(warpSettingsTunnelProtocolEl, '--');
    return;
  }
  if (settings.error) {
    setText(warpSettingsStateEl, `加载失败：${settings.error}`);
    setText(warpSettingsModeEl, '--');
    setText(warpSettingsTunnelProtocolEl, '--');
    return;
  }
  setText(warpSettingsStateEl, '已加载');
  setText(warpSettingsModeEl, fmtSettingValue(settings.mode));
  setText(warpSettingsTunnelProtocolEl, fmtSettingValue(settings.tunnelProtocol));
}

function syncAdvancedControls(network, force = false) {
  syncStackModeState(network);
  syncWarpState();
}

function syncEasyModeState(network) {
  if (!easyModeToggleEl || !easyModeStateEl) {
    return;
  }
  if (pendingToggleState.easyMode) {
    setText(easyModeStateEl, pendingToggleState.easyMode.enabled ? '正在开启...' : '正在关闭...');
    return;
  }
  const ifName = currentIfName();
  const adapter = Array.isArray(network?.adapters)
    ? network.adapters.find(item => item && item.name === ifName)
    : null;
  const ipv6Only = !!adapter?.ipv6Enabled && !adapter?.ipv4Enabled;
  const enabled = warpConnected && ipv6Only;
  easyModeToggleEl.checked = enabled;
  setText(easyModeStateEl, enabled ? '当前已开启' : '当前关闭');
  updateFreeFlowBadge();
}

function currentIfName() {
  const value = targetAdapterSelects[1]?.value?.trim() || targetAdapterSelects[0]?.value?.trim();
  return value || 'WiFi';
}

function getAdapterOptions(network) {
  const names = Array.isArray(network?.availableAdapters) ? network.availableAdapters : [];
  if (names.length > 0) {
    return names.filter(name => typeof name === 'string' && name.trim() !== '');
  }
  return Array.isArray(network?.adapters)
    ? network.adapters.map(adapter => adapter?.name).filter(name => typeof name === 'string' && name.trim() !== '')
    : [];
}

function syncTargetAdapterSelects(names) {
  const options = Array.isArray(names) ? names : [];
  const current = currentIfName();
  const desired = options.includes(current) ? current : (options[0] || 'WiFi');

  for (const select of targetAdapterSelects) {
    if (!select) continue;
    const previous = select.value;
    select.innerHTML = '';
    if (options.length === 0) {
      const option = document.createElement('option');
      option.value = 'WiFi';
      option.textContent = 'WiFi';
      select.appendChild(option);
      continue;
    }
    for (const name of options) {
      const option = document.createElement('option');
      option.value = name;
      option.textContent = name;
      select.appendChild(option);
    }
    select.value = options.includes(previous) ? previous : desired;
  }

  const selected = options.includes(desired) ? desired : (options[0] || 'WiFi');
  setTargetAdapter(selected);
}

function renderStatus(data, force = false) {
  syncTargetAdapterSelects(getAdapterOptions(data?.network));
  syncWarpSettingsState(data?.network);
  renderNetwork(data?.network);
  updateStatusBadges(data?.network, force);
  syncDnsEditor(data?.network, force);
  syncDns64ModeState(data?.network, force);
}

function fmtIPList(arr) {
  if (!Array.isArray(arr) || arr.length === 0) {
    return 'none';
  }
  return arr.join('\n');
}

function fmtOne(value) {
  if (typeof value !== 'string' || value.trim() === '') {
    return 'none';
  }
  return value.trim();
}

function fmtTrafficValue(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number.toFixed(2) : '-';
}

function renderTrafficUsage(payload) {
  trafficUsageEl.innerHTML = `v4流量：${fmtTrafficValue(payload?.data?.v4)} MB<br />v6流量：${fmtTrafficValue(payload?.data?.v6)} MB`;
  setText(lastUpdatedEl, new Date().toLocaleString());
}

function loadTrafficUsage() {
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

function refreshTrafficUsage() {
  return loadTrafficUsage()
    .then((payload) => {
      renderTrafficUsage(payload);
      return payload;
    })
    .catch(() => {});
}

function renderNetwork(network) {
  latestNetwork = network;
  syncTargetAdapterSelects(getAdapterOptions(network));
  adapterListEl.innerHTML = '';
  const adapters = network?.adapters;
  if (!Array.isArray(adapters) || adapters.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'kv';
    empty.innerHTML = '<span>网卡状态</span><strong>暂无数据</strong>';
    adapterListEl.appendChild(empty);
    return;
  }

  for (const adapter of adapters) {
    const card = document.createElement('article');
    card.className = 'adapter-card';
    card.innerHTML = `
      <div class="adapter-head">
        <div class="adapter-name">${adapter.name ?? '-'}</div>
        <span class="pill">${adapter.status ?? 'unknown'}</span>
      </div>
      <div class="adapter-meta">${adapter.description ?? ''}</div>
      <div class="adapter-meta">MAC: ${adapter.macAddress ?? '-'}</div>
      <div class="adapter-meta">IPv4 GW: ${fmtOne(adapter.ipv4Gateway)}</div>
      <div class="adapter-meta">IPv6 GW: ${fmtOne(adapter.ipv6Gateway)}</div>
      <div class="ip-block">DNS\n${fmtIPList(adapter.dns)}</div>
      <div class="stack-row">
        <span class="stack-pill ${adapter.ipv4Enabled ? 'on' : 'off'}">IPv4 ${adapter.ipv4Enabled ? 'ON' : 'OFF'}</span>
        <span class="stack-pill ${adapter.ipv6Enabled ? 'on' : 'off'}">IPv6 ${adapter.ipv6Enabled ? 'ON' : 'OFF'}</span>
      </div>
      <div class="ip-block">IPv4\n${fmtIPList(adapter.ipv4)}</div>
      <div class="ip-block">IPv6\n${fmtIPList(adapter.ipv6)}</div>
    `;
    adapterListEl.appendChild(card);
  }
}

function updateStatusBadges(network, force = false) {
  const online = settleBoolState(badgeState.network, !!network?.online, force);
  setNetworkBadge(online ? 'ok' : 'warn');
  syncAdvancedControls(network, force);
  syncEasyModeState(network);
}

async function refreshStatus(force = false) {
  try {
    const res = await fetch('/api/v1/status');
    const data = await res.json();
    renderStatus(data, force);
    appendLog('状态已刷新');
  } catch (err) {
    appendLog(`状态刷新失败：${err.message}`);
  }
}

async function checkAdminStatus() {
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

async function postAction(path, body) {
  setBusy(true);
  try {
    const res = await fetch(path, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    setText(lastResultEl, JSON.stringify(data, null, 2));
    appendLog(`${path} -> ${data.ok ? 'ok' : 'error'}`);
    if (!res.ok) {
      throw new Error(data.error || 'request failed');
    }
    return data;
  } catch (err) {
    setText(lastResultEl, err.message);
    appendLog(`请求失败：${err.message}`);
    throw err;
  } finally {
    setBusy(false);
  }
}

async function postActionSilently(path, body) {
  const res = await fetch(path, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  });
  const data = await res.json();
  setText(lastResultEl, JSON.stringify(data, null, 2));
  appendLog(`${path} -> ${data.ok ? 'ok' : 'error'}`);
  if (!res.ok) {
    throw new Error(data.error || 'request failed');
  }
  return data;
}

async function switchStack(mode) {
  const ifName = currentIfName();
  const stackLabel = {
    ipv4: 'IP 栈协议（仅 v4）',
    ipv6: 'IP 栈协议（仅 v6）',
    both: 'IP 栈协议（双栈）',
  }[mode] || 'IP 栈协议';
  showConfigurationToast(stackLabel);
  try {
    await postAction('/api/v1/switch', { ifName, mode });
    lastSwitchTime = Date.now();
    applyOptimisticNetworkState({ adapterMode: mode });
    showConfigurationSuccess();
  } catch (err) {
    showConfigurationFailure(err.message);
    throw err;
  }
}

async function applyWarpToggle(enabled) {
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

  if (enabled && dns64ModeToggleEl && isDns64ModeActive(getSelectedAdapter(latestNetwork))) {
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
      checkIpv6Address().catch(() => {});
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

async function applyEasyMode(enabled) {
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
    syncEasyModeState(latestNetwork);
    if (succeeded && !enabled) {
      showConfigurationSuccess();
    } else if (succeeded && enabled) {
      easyModePendingSuccess = true;
      showConfigurationToast('免流模式', '已开启，等待 WARP 连接...');
    }
  }
}

let lastNetworkCollectedAt = '';
let lastSwitchTime = 0;

function connectWS() {
  const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${scheme}://${location.host}/ws`);

  ws.addEventListener('open', () => {
    setBackendBadge('ok');
    appendLog('WebSocket 已连接');
  });

  ws.addEventListener('message', (event) => {
    try {
      const data = JSON.parse(event.data);
      setBackendBadge(data.type === 'heartbeat' || data.type === 'hello' || data.type === 'network.status' ? 'ok' : (data.type === 'error' ? 'err' : 'ok'));
      if (data.type === 'network.status') {
        if (Date.now() - lastSwitchTime < 3000) {
          return;
        }
        const collectedAt = data.data?.collectedAt || '';
        if (collectedAt && collectedAt <= lastNetworkCollectedAt) {
          return;
        }
        lastNetworkCollectedAt = collectedAt;
        renderStatus({
          service: { name: 'BKNetwork' },
          lastEvent: { type: data.type, message: data.message },
          network: data.data,
        });
        setText(lastUpdatedEl, `${new Date().toLocaleTimeString()}`);
        markInitializationComplete();
        return;
      }
      if (data.type === 'warp.ok') {
        if (pendingToggleState.warp && pendingToggleState.warp.finishOnEvent === 'warp.ok') {
          clearPendingToggle('warp');
        }
        if (pendingToggleState.easyMode && pendingToggleState.easyMode.finishOnEvent === 'warp.ok') {
          clearPendingToggle('easyMode');
        }
        return;
      }
      if (data.type === 'switch.ok') {
        if (pendingToggleState.easyMode && pendingToggleState.easyMode.finishOnEvent === 'switch.ok') {
          applyOptimisticNetworkState({ warpConnected: false, adapterMode: 'both' });
          clearPendingToggle('easyMode');
          easyModeToggleEl.checked = false;
          setText(easyModeStateEl, '当前关闭');
          showConfigurationSuccess();
        }
        return;
      }
      if (data.type === 'heartbeat') {
        return;
      }
      if (data.type === 'hello') {
        return;
      }
      setText(lastResultEl, JSON.stringify(data, null, 2));
      appendLog(`${data.type} · ${data.message}`);
    } catch {
      appendLog(String(event.data));
    }
  });

  ws.addEventListener('close', () => {
    setBackendBadge('err');
    appendLog('WebSocket 已断开，准备重连');
    setTimeout(connectWS, 2000);
  });

  ws.addEventListener('error', () => {
    setBackendBadge('warn');
  });
}

if (advancedModeToggleEl) {
  advancedModeToggleEl.addEventListener('change', () => setAdvancedMode(advancedModeToggleEl.checked));
}

if (easyModeToggleEl) {
  easyModeToggleEl.addEventListener('change', () => {
    applyEasyMode(easyModeToggleEl.checked).catch(() => {});
  });
}

if (dns64ModeToggleEl) {
  dns64ModeToggleEl.addEventListener('change', () => {
    applyDns64Mode(dns64ModeToggleEl.checked).catch(() => {});
  });
}

if (warpToggleEl) {
  warpToggleEl.addEventListener('change', () => {
    applyWarpToggle(warpToggleEl.checked).catch(() => {});
  });
}

if (settingsOpenBtn) {
  settingsOpenBtn.addEventListener('click', () => {
    openSettingsPanel().catch(() => {});
  });
}

if (ipv6RefreshBtnEl) {
  ipv6RefreshBtnEl.addEventListener('click', () => {
    checkIpv6Address().catch(() => {});
  });
}

if (dnsIpv4InputEl) {
  dnsIpv4InputEl.addEventListener('input', () => {
    dnsEditorState.dirty.ipv4 = dnsIpv4InputEl.value !== dnsEditorState.committed.ipv4;
    syncDnsEditor(latestNetwork);
  });
  dnsIpv4InputEl.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      saveDnsEditor().catch(() => {});
    }
  });
}

if (dnsIpv6InputEl) {
  dnsIpv6InputEl.addEventListener('input', () => {
    dnsEditorState.dirty.ipv6 = dnsIpv6InputEl.value !== dnsEditorState.committed.ipv6;
    syncDnsEditor(latestNetwork);
  });
  dnsIpv6InputEl.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      saveDnsEditor().catch(() => {});
    }
  });
}

if (dnsCardEl) {
  dnsCardEl.addEventListener('focusout', (event) => {
    if (dnsEditorState.saving) {
      return;
    }
    const nextTarget = event.relatedTarget;
    if (nextTarget && dnsCardEl.contains(nextTarget)) {
      return;
    }
    if (dnsEditorState.dirty.ipv4 || dnsEditorState.dirty.ipv6) {
      saveDnsEditor().catch(() => {});
    }
  });
}

if (settingsCloseBtn) {
  settingsCloseBtn.addEventListener('click', () => setSettingsOpen(false));
}

if (settingsOverlayEl) {
  settingsOverlayEl.addEventListener('click', (event) => {
    if (event.target === settingsOverlayEl) {
      setSettingsOpen(false);
    }
  });
}

if (settingAutoStartEl) {
  settingAutoStartEl.addEventListener('change', () => {
    settingsState.autoStart = settingAutoStartEl.checked;
    saveSettings();
  });
}

if (settingSilentStartEl) {
  settingSilentStartEl.addEventListener('change', () => {
    settingsState.silentStart = settingSilentStartEl.checked;
    saveSettings();
  });
}

if (settingWarpAutoStartEl) {
  settingWarpAutoStartEl.addEventListener('change', () => {
    settingsState.warpAutoStart = settingWarpAutoStartEl.checked;
    saveSettings();
  });
}

if (settingWarpAppAutoStartEl) {
  settingWarpAppAutoStartEl.addEventListener('change', () => {
    settingsState.warpAppAutoStart = settingWarpAppAutoStartEl.checked;
    saveSettings();
  });
}

window.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') {
    setSettingsOpen(false);
  }
});

setAdvancedMode(false);
showInitializationToast();

refreshStatus(true);
startWarpStatusPoll();
window.addEventListener('load', () => {
  scheduleDeferredStartupTasks();
  checkAdminStatus().catch(() => {});
}, { once: true });
setInterval(refreshTrafficUsage, 60000);
connectWS();
