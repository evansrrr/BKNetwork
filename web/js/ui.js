import { adapterListEl, adminBadgeDotEl, adminBadgeEl, advancedModeToggleEl, appVersionEl, backendBadgeDotEl, buttons, dns64ModeStateEl, dns64ModeToggleEl, dnsIpv4InputEl, dnsIpv6InputEl, dnsStatusEl, easyModeStateEl, easyModeToggleEl, freeBadgeDotEl, ipv6CheckDotEl, ipv6CheckTextEl, lastUpdatedEl, logEl, networkBadgeDotEl, operationToastDescEl, operationToastEl, operationToastTitleEl, settingAutoStartEl, settingDisableOnlineChecksEl, settingSilentStartEl, settingWarpAppAutoStartEl, settingsOverlayEl, settingsStatusEl, stackModeButtons, stackModeStateEl, targetAdapterSelects, trafficUsageEl, warpSettingsModeEl, warpSettingsStateEl, warpSettingsTunnelProtocolEl, warpStateEl, warpToggleEl } from './dom.js';
import { DNS64_CANDIDATES, badgeState, dns64ModeState, dnsEditorState, operationToastState, pendingToggleState, settingsState, store } from './state.js';
import { checkForNewVersion, startFastStatusRefresh, stopFastStatusRefresh } from './actions.js';

export function setTargetAdapter(value) {
  const next = typeof value === 'string' && value.trim() !== '' ? value.trim() : 'WiFi';
  for (const select of targetAdapterSelects) {
    if (select) {
      select.value = next;
    }
  }
  syncEasyModeState(store.latestNetwork);
  syncDnsEditor(store.latestNetwork);
  syncDns64ModeState(store.latestNetwork);
}

for (const select of targetAdapterSelects) {
  if (!select) continue;
  select.addEventListener('change', () => setTargetAdapter(select.value));
}

export function setBusy(busy) {
  buttons.forEach(btn => btn.disabled = busy);
}

export function getStableWarpValue() {
  return store.warpConnected;
}

export function getStableEasyModeValue() {
  const adapter = getSelectedAdapter(store.latestNetwork);
  return !!store.latestNetwork?.warp?.connected && !!adapter?.freeFlow;
}

export function clearPendingToggle(kind) {
  pendingToggleState[kind] = null;
  if (!pendingToggleState.warp && !pendingToggleState.easyMode) {
    stopFastStatusRefresh();
  }
}

export function markPendingToggle(kind, enabled, finishOnEvent) {
  pendingToggleState[kind] = { enabled, finishOnEvent };
  startFastStatusRefresh();
}

export function settleBoolState(state, next, force = false) {
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

export function applyOptimisticNetworkState({ adapterMode } = {}) {
  if (!store.latestNetwork) {
    return;
  }

  const targetName = currentIfName();
  const nextAdapters = Array.isArray(store.latestNetwork.adapters)
    ? store.latestNetwork.adapters.map(adapter => {
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
        if (typeof store.warpConnected === 'boolean') {
          nextAdapter.freeFlow = store.warpConnected && !!nextAdapter.ipv6Enabled && !nextAdapter.ipv4Enabled;
        }
        return nextAdapter;
      })
    : store.latestNetwork.adapters;

  store.latestNetwork = {
    ...store.latestNetwork,
    warp: typeof store.warpConnected === 'boolean'
      ? { ...(store.latestNetwork.warp || {}), connected: store.warpConnected }
      : store.latestNetwork.warp,
    adapters: nextAdapters,
  };
  renderStatus({
    service: { name: 'BKNetwork' },
    lastEvent: { type: 'network.status', message: 'optimistic update' },
    network: store.latestNetwork,
  }, true);
}

export function getAdapterMode(adapter) {
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

export function setAdvancedMode(enabled) {
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

export function setDot(dotEl, tone) {
  if (dotEl) dotEl.className = `dot ${tone || ''}`.trim();
}

export function setText(el, value) {
  if (el) {
    el.textContent = value;
  }
}

export function setSettingsStatus(value) {
  setText(settingsStatusEl, value);
}

export function setDnsStatus(value) {
  setText(dnsStatusEl, value);
}

export function setDns64ModeStatus(value) {
  setText(dns64ModeStateEl, value);
}

export function clearOperationToastTimer() {
  if (operationToastState.hideTimer !== null) {
    clearTimeout(operationToastState.hideTimer);
    operationToastState.hideTimer = null;
  }
}

export function showOperationToast(title, desc = '', tone = 'info', autoHideMs = 0) {
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

export function showInitializationToast() {
  if (operationToastState.initializationDone) {
    return;
  }
  showOperationToast('正在初始化...', '正在连接 WebSocket 并同步页面状态', 'info');
}

export function markInitializationComplete() {
  if (operationToastState.initializationDone) {
    return;
  }
  operationToastState.initializationDone = true;
  showOperationToast('初始化成功！', '页面状态已同步完成', 'success', 1800);
  if (!operationToastState.versionCheckStarted) {
    operationToastState.versionCheckStarted = true;
    window.setTimeout(() => {
      checkForNewVersion().catch(err => console.error('操作失败:', err));
    }, 0);
  }
}

export function showConfigurationToast(label, detail) {
  showOperationToast(`正在配置${label}...`, detail || '', 'info');
}

export function showConfigurationSuccess() {
  showOperationToast('配置成功！', '页面状态即将同步更新', 'success', 1600);
}

export function showConfigurationFailure(message) {
  showOperationToast('配置失败', message || '请稍后重试', 'warn', 2400);
}

export function getCurrentVersionText() {
  return appVersionEl?.textContent?.trim() || '';
}

export function parseVersion(version) {
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

export function compareVersions(leftVersion, rightVersion) {
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

export function syncSettingsControls() {
  if (settingAutoStartEl) settingAutoStartEl.checked = !!settingsState.autoStart;
  if (settingSilentStartEl) settingSilentStartEl.checked = !!settingsState.silentStart;
  if (settingWarpAppAutoStartEl) settingWarpAppAutoStartEl.checked = !!settingsState.warpAppAutoStart;
  if (settingDisableOnlineChecksEl) settingDisableOnlineChecksEl.checked = !!settingsState.disableOnlineChecks;
}

export function setSettingsOpen(open) {
  if (!settingsOverlayEl) return;
  settingsOverlayEl.classList.toggle('open', !!open);
  settingsOverlayEl.setAttribute('aria-hidden', String(!open));
  if (open) {
    syncSettingsControls();
  }
}

export function appendLog(line) {
  const stamp = new Date().toLocaleTimeString();
  if (logEl) {
    logEl.textContent = `[${stamp}] ${line}\n` + logEl.textContent;
  }
}

export function setBackendBadge(tone) {
  setDot(backendBadgeDotEl, tone);
}

export function setAdminBadge(tone, text) {
  setDot(adminBadgeDotEl, tone);
  setText(adminBadgeEl, text);
}

export function setNetworkBadge(tone) {
  setDot(networkBadgeDotEl, tone);
}

export function setFreeBadge(tone) {
  setDot(freeBadgeDotEl, tone);
}

export function formatDnsServers(values) {
  return Array.isArray(values) && values.length > 0 ? values.join(', ') : '';
}

export function parseDnsServers(value) {
  return String(value || '')
    .split(/[\s,;]+/)
    .map(item => item.trim())
    .filter(Boolean);
}

export function splitDnsServers(values) {
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

export function pickDns64Resolver() {
  return DNS64_CANDIDATES[Math.floor(Math.random() * DNS64_CANDIDATES.length)];
}

export function isDns64ModeActive(adapter) {
  if (!adapter || !adapter.ipv6Enabled || adapter.ipv4Enabled) {
    return false;
  }
  const { ipv6 } = splitDnsServers(adapter.dns);
  return ipv6.length === 1 && DNS64_CANDIDATES.includes(ipv6[0]);
}

// 开启 DNS64 前的原始 IPv6 DNS 备份。保存在 localStorage 里，避免刷新或重启后
// 找不到原值，从而把用户的 DNS 永久留在 DNS64 解析器上。

export function getDns64Backup(adapter) {
  const { ipv6 } = splitDnsServers(adapter?.dns);
  // 已经指向 DNS64 解析器的值不算“原始配置”，否则反复开关会把备份覆盖成解析器。
  return ipv6.filter(server => !DNS64_CANDIDATES.includes(server));
}

export function getSelectedAdapter(network) {
  const ifName = currentIfName();
  return Array.isArray(network?.adapters)
    ? network.adapters.find(item => item && item.name === ifName)
    : null;
}

export function syncDnsEditor(network, force = false) {
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

export function syncDns64ModeState(network, force = false) {
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

export function isFreeFlowModeActive(adapter) {
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
  return store.warpConnected && ipv6Only;
}

export function setIpv6CheckStatus(tone, text) {
  setDot(ipv6CheckDotEl, tone);
  setText(ipv6CheckTextEl, text);
}

export function delay(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

export function findLocalIpv6Address(network) {
  for (const adapter of network?.adapters || []) {
    for (const address of adapter.ipv6 || []) {
      const value = String(address || '').trim();
      if (!value || value.startsWith('fe80:') || value.startsWith('::1')) {
        continue;
      }
      return value;
    }
  }
  return '';
}

export function syncStackModeState(network) {
  const adapter = getSelectedAdapter(network);
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

export function updateFreeFlowBadge() {
  const adapter = getSelectedAdapter(store.latestNetwork);
  setFreeBadge(isFreeFlowModeActive(adapter) ? 'ok' : 'warn');
}

export function syncWarpState() {
  if (!warpToggleEl || !warpStateEl) {
    return;
  }
  if (pendingToggleState.warp) {
    setText(warpStateEl, pendingToggleState.warp.enabled ? '正在开启...' : '正在关闭...');
    return;
  }
  warpToggleEl.checked = store.warpConnected;
  setText(warpStateEl, store.warpConnected ? '当前：已开启' : store.warpStatusText_);
}

export function fmtSettingValue(value, fallback = '--') {
  return typeof value === 'string' && value.trim() !== '' ? value.trim() : fallback;
}

export function syncWarpSettingsState(network) {
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

export function syncAdvancedControls(network, force = false) {
  syncStackModeState(network);
  syncWarpState();
}

export function syncEasyModeState(network) {
  if (!easyModeToggleEl || !easyModeStateEl) {
    return;
  }
  if (pendingToggleState.easyMode) {
    setText(easyModeStateEl, pendingToggleState.easyMode.enabled ? '正在开启...' : '正在关闭...');
    return;
  }
  const adapter = getSelectedAdapter(network);
  const ipv6Only = !!adapter?.ipv6Enabled && !adapter?.ipv4Enabled;
  const enabled = store.warpConnected && ipv6Only;
  easyModeToggleEl.checked = enabled;
  setText(easyModeStateEl, enabled ? '当前已开启' : '当前关闭');
  updateFreeFlowBadge();
}

export function currentIfName() {
  const value = targetAdapterSelects[1]?.value?.trim() || targetAdapterSelects[0]?.value?.trim();
  return value || 'WiFi';
}

export function isWarpAdapterName(name) {
  if (typeof name !== 'string') return false;
  const n = name.trim().toLowerCase();
  return n.includes('warp') || n.includes('cloudflare');
}

export function getAdapterOptions(network) {
  const names = Array.isArray(network?.availableAdapters) ? network.availableAdapters : [];
  if (names.length > 0) {
    return names.filter(name => typeof name === 'string' && name.trim() !== '');
  }
  return Array.isArray(network?.adapters)
    ? network.adapters.map(adapter => adapter?.name).filter(name => typeof name === 'string' && name.trim() !== '')
    : [];
}

// pickDefaultAdapter chooses the best fallback when the current selection is unavailable.
// Prefer non-WARP physical adapters so auto-selection does not land on the tunnel.
export function pickDefaultAdapter(options) {
  if (!Array.isArray(options) || options.length === 0) {
    return 'WiFi';
  }
  const physical = options.find(name => !isWarpAdapterName(name));
  return physical || options[0] || 'WiFi';
}

export function syncTargetAdapterSelects(names) {
  const options = Array.isArray(names) ? names : [];
  const signature = options.join('\n');
  if (signature === store.lastAdapterOptionsSignature) {
    return;
  }
  store.lastAdapterOptionsSignature = signature;
  const current = currentIfName();
  const desired = options.includes(current) ? current : pickDefaultAdapter(options);

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

  const selected = options.includes(desired) ? desired : pickDefaultAdapter(options);
  setTargetAdapter(selected);
}

export function renderStatus(data, force = false) {
  const version = typeof data?.service?.version === 'string' ? data.service.version.trim() : '';
  if (version && version !== 'dev') {
    setText(appVersionEl, `v${version.replace(/^v/, '')}`);
  }
  syncTargetAdapterSelects(getAdapterOptions(data?.network));
  syncWarpSettingsState(data?.network);
  renderNetwork(data?.network);
  updateStatusBadges(data?.network, force);
  syncDnsEditor(data?.network, force);
  syncDns64ModeState(data?.network, force);
}

export function fmtIPList(arr) {
  if (!Array.isArray(arr) || arr.length === 0) {
    return 'none';
  }
  return arr.join('\n');
}

export function fmtOne(value) {
  if (typeof value !== 'string' || value.trim() === '') {
    return 'none';
  }
  return value.trim();
}

export function escapeHTML(str) {
  if (str == null) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

export function fmtTrafficValue(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number.toFixed(2) : '-';
}

export function renderTrafficUsage(payload) {
  trafficUsageEl.innerHTML = `v4流量：${fmtTrafficValue(payload?.data?.v4)} MB<br />v6流量：${fmtTrafficValue(payload?.data?.v6)} MB`;
  setText(lastUpdatedEl, new Date().toLocaleString());
}

export function renderNetwork(network) {
  store.latestNetwork = network;
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
        <div class="adapter-name">${escapeHTML(adapter.name) || '-'}</div>
        <span class="pill">${escapeHTML(adapter.status) || 'unknown'}</span>
      </div>
      <div class="adapter-meta">${escapeHTML(adapter.description) || ''}</div>
      <div class="adapter-meta">MAC: ${escapeHTML(adapter.macAddress) || '-'}</div>
      <div class="adapter-meta">IPv4 GW: ${escapeHTML(fmtOne(adapter.ipv4Gateway))}</div>
      <div class="adapter-meta">IPv6 GW: ${escapeHTML(fmtOne(adapter.ipv6Gateway))}</div>
      <div class="ip-block">DNS\n${escapeHTML(fmtIPList(adapter.dns))}</div>
      <div class="stack-row">
        <span class="stack-pill ${adapter.ipv4Enabled ? 'on' : 'off'}">IPv4 ${adapter.ipv4Enabled ? 'ON' : 'OFF'}</span>
        <span class="stack-pill ${adapter.ipv6Enabled ? 'on' : 'off'}">IPv6 ${adapter.ipv6Enabled ? 'ON' : 'OFF'}</span>
      </div>
      <div class="ip-block">IPv4\n${escapeHTML(fmtIPList(adapter.ipv4))}</div>
      <div class="ip-block">IPv6\n${escapeHTML(fmtIPList(adapter.ipv6))}</div>
    `;
    adapterListEl.appendChild(card);
  }
}

export function updateStatusBadges(network, force = false) {
  const online = settleBoolState(badgeState.network, !!network?.online, force);
  setNetworkBadge(online ? 'ok' : 'warn');
  syncAdvancedControls(network, force);
  syncEasyModeState(network);
}
