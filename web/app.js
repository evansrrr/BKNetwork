import { advancedModeToggleEl, dns64ModeToggleEl, dnsCardEl, dnsIpv4InputEl, dnsIpv6InputEl, easyModeStateEl, easyModeToggleEl, ipv6RefreshBtnEl, lastUpdatedEl, settingAutoStartEl, settingDisableOnlineChecksEl, settingSilentStartEl, settingWarpAppAutoStartEl, settingsCloseBtn, settingsDiagnosticsBtn, settingsOpenBtn, settingsOverlayEl, warpToggleEl } from './js/dom.js';
import { dnsEditorState, pendingToggleState, settingsState, store } from './js/state.js';
import { appendLog, applyOptimisticNetworkState, clearPendingToggle, delay, markInitializationComplete, renderStatus, setAdvancedMode, setBackendBadge, setSettingsOpen, setText, showConfigurationSuccess, showInitializationToast, syncDnsEditor } from './js/ui.js';
import { applyDns64Mode, applyEasyMode, applyWarpToggle, checkAdminStatus, checkIpv6Address, exportDiagnostics, openSettingsPanel, refreshStatus, refreshTrafficUsage, saveDnsEditor, saveSettings, scheduleDeferredStartupTasks, startWarpStatusPoll, switchStack } from './js/actions.js';

// connectWS is defined below, near the initialization block.

if (advancedModeToggleEl) {
  advancedModeToggleEl.addEventListener('change', () => setAdvancedMode(advancedModeToggleEl.checked));
}

if (easyModeToggleEl) {
  easyModeToggleEl.addEventListener('change', () => {
    applyEasyMode(easyModeToggleEl.checked).catch(err => console.error('操作失败:', err));
  });
}

if (dns64ModeToggleEl) {
  dns64ModeToggleEl.addEventListener('change', () => {
    applyDns64Mode(dns64ModeToggleEl.checked).catch(err => console.error('操作失败:', err));
  });
}

if (warpToggleEl) {
  warpToggleEl.addEventListener('change', () => {
    applyWarpToggle(warpToggleEl.checked).catch(err => console.error('操作失败:', err));
  });
}

if (settingsOpenBtn) {
  settingsOpenBtn.addEventListener('click', () => {
    openSettingsPanel().catch(err => console.error('操作失败:', err));
  });
}

if (ipv6RefreshBtnEl) {
  ipv6RefreshBtnEl.addEventListener('click', () => {
    checkIpv6Address().catch(err => console.error('操作失败:', err));
  });
}

if (dnsIpv4InputEl) {
  dnsIpv4InputEl.addEventListener('input', () => {
    dnsEditorState.dirty.ipv4 = dnsIpv4InputEl.value !== dnsEditorState.committed.ipv4;
    syncDnsEditor(store.latestNetwork);
  });
  dnsIpv4InputEl.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      saveDnsEditor().catch(err => console.error('操作失败:', err));
    }
  });
}

if (dnsIpv6InputEl) {
  dnsIpv6InputEl.addEventListener('input', () => {
    dnsEditorState.dirty.ipv6 = dnsIpv6InputEl.value !== dnsEditorState.committed.ipv6;
    syncDnsEditor(store.latestNetwork);
  });
  dnsIpv6InputEl.addEventListener('keydown', (event) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      saveDnsEditor().catch(err => console.error('操作失败:', err));
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
      saveDnsEditor().catch(err => console.error('操作失败:', err));
    }
  });
}

if (settingsCloseBtn) {
  settingsCloseBtn.addEventListener('click', () => setSettingsOpen(false));
}

if (settingsDiagnosticsBtn) {
  settingsDiagnosticsBtn.addEventListener('click', () => {
    exportDiagnostics().catch(err => console.error('操作失败:', err));
  });
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

if (settingWarpAppAutoStartEl) {
  settingWarpAppAutoStartEl.addEventListener('change', () => {
    settingsState.warpAppAutoStart = settingWarpAppAutoStartEl.checked;
    saveSettings();
  });
}

if (settingDisableOnlineChecksEl) {
  settingDisableOnlineChecksEl.addEventListener('change', () => {
    settingsState.disableOnlineChecks = settingDisableOnlineChecksEl.checked;
    saveSettings();
  });
}

window.addEventListener('keydown', (event) => {
  if (event.key === 'Escape') {
    setSettingsOpen(false);
  }
});

let activeWs = null;
let wsReconnectAttempt = 0;

// 指数退避 + 抖动：服务重启或崩溃时不要每 2 秒集中重连一次
function nextReconnectDelay() {
  const base = Math.min(30000, 1000 * 2 ** wsReconnectAttempt);
  wsReconnectAttempt = Math.min(wsReconnectAttempt + 1, 5);
  return Math.round(base * (0.5 + Math.random() * 0.5));
}

function connectWS() {
  const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${scheme}://${location.host}/ws`);
  activeWs = ws;

  ws.addEventListener('open', () => {
    wsReconnectAttempt = 0;
    setBackendBadge('ok');
    appendLog('WebSocket 已连接');
  });

  ws.addEventListener('message', (event) => {
    try {
      const data = JSON.parse(event.data);
      setBackendBadge(data.type === 'error' ? 'err' : 'ok');
      if (data.type === 'network.status') {
        if (Date.now() - store.lastSwitchTime < 3000) {
          return;
        }
        const collectedAt = data.data?.collectedAt || '';
        if (collectedAt && collectedAt <= store.lastNetworkCollectedAt) {
          return;
        }
        store.lastNetworkCollectedAt = collectedAt;
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
          applyOptimisticNetworkState({ adapterMode: 'both' });
          clearPendingToggle('easyMode');
          easyModeToggleEl.checked = false;
          setText(easyModeStateEl, '当前关闭');
          showConfigurationSuccess();
        }
        return;
      }
      if (data.type === 'hello') {
        return;
      }
      appendLog(`${data.type} · ${data.message}`);
    } catch (err) {
      console.error('WebSocket 消息解析失败:', err);
      appendLog(String(event.data));
    }
  });

  ws.addEventListener('close', () => {
    if (activeWs === ws) {
      activeWs = null;
    }
    setBackendBadge('err');
    const delay = nextReconnectDelay();
    appendLog(`WebSocket 已断开，${Math.round(delay / 1000)}s 后重连`);
    setTimeout(connectWS, delay);
  });

  ws.addEventListener('error', () => {
    setBackendBadge('warn');
  });
}

setAdvancedMode(false);
showInitializationToast();

// 引导 token 只用于换取会话 Cookie，立即从地址栏和历史记录中去掉
if (location.search) {
  const params = new URLSearchParams(location.search);
  if (params.has('token')) {
    params.delete('token');
    const query = params.toString();
    history.replaceState(null, '', location.pathname + (query ? '?' + query : '') + location.hash);
  }
}

// 切换按钮通过 data 属性绑定，不依赖内联 onclick（CSP 禁止内联脚本）
for (const button of document.querySelectorAll('[data-switch-mode]')) {
  button.addEventListener('click', () => {
    switchStack(button.dataset.switchMode).catch(() => {});
  });
}

refreshStatus(true);
startWarpStatusPoll();
window.addEventListener('load', () => {
  scheduleDeferredStartupTasks();
  checkAdminStatus().catch(err => console.error('操作失败:', err));
}, { once: true });
setInterval(refreshTrafficUsage, 60000);
connectWS();

// 拦截外部链接点击，在系统浏览器中打开
// 由于 webview 加载的是外部 URL，Tauri JS API 不可用
// 这里使用 window.open 触发 on_new_window 回调，由 Rust 端处理
document.addEventListener('click', (event) => {
  const link = event.target.closest('a[href]');
  if (!link) return;

  const href = link.getAttribute('href');
  if (!href) return;

  // 仅处理 http/https 链接
  if (!href.startsWith('http://') && !href.startsWith('https://')) return;

  // 后端链接允许在 webview 内正常导航
  try {
    const url = new URL(href, location.origin);
    if (url.hostname === '127.0.0.1' || url.hostname === 'localhost') return;
  } catch {
    return;
  }

  // 阻止默认导航，改为 window.open 触发 on_new_window
  event.preventDefault();
  event.stopPropagation();
  window.open(href, '_blank');
}, true);
