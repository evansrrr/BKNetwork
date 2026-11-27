export const badgeState = {
  network: { stable: null, pending: null },
  warp: { stable: null, pending: null },
};
export const settingsState = {
  autoStart: false,
  silentStart: false,
  warpAutoStart: false,
  warpAppAutoStart: false,
  disableOnlineChecks: false,
  loaded: false,
  saving: false,
};
export const operationToastState = {
  hideTimer: null,
  initializationDone: false,
  versionCheckStarted: false,
};
export const fastStatusRefreshState = {
  intervalId: null,
  stopTimerId: null,
  activeUntil: 0,
};
export const pendingToggleState = {
  warp: null,
  easyMode: null,
};
export const dns64ModeState = {
  pending: null,
  backupIpv6Servers: [],
};
export const dnsEditorState = {
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

// store holds the mutable scalars that several modules read and reassign.
// ES module exports are read-only bindings, so the former let-variables live
// on this one exported object instead.
export const store = {
  latestNetwork: null,
  warpConnected: false,
  warpStatusText_: '',
  easyModePendingSuccess: false,
  lastAdapterOptionsSignature: null,
  lastNetworkCollectedAt: '',
  // 切换保护窗口用 performance.now()（单调时钟）：壁钟会被休眠唤醒后的
  // NTP 校正向前/向后跳，导致保护窗口意外失效或长时间卡住。
  lastSwitchAtMonotonic: 0,
};

export const DNS64_CANDIDATES = [
  '2a00:1098:2b::1',
  '2a01:4ff:f0:9876::1',
  '2a01:4f9:c010:3f02::1',
  '2a00:1098:2c::1',
  '2a01:4f8:c2c:123f::1',
];

export const DNS64_BACKUP_KEY = 'bknetwork.dns64Backup';
export const TARGET_ADAPTER_KEY = 'bknetwork.targetAdapter';

// Target adapter selection is persisted so a refresh or a restart keeps the
// user on the adapter they picked. The backend rejects wildcards and virtual
// adapters, so this only stores an exact adapter name.
export function readTargetAdapter() {
  try {
    const raw = localStorage.getItem(TARGET_ADAPTER_KEY);
    return typeof raw === 'string' ? raw.trim() : '';
  } catch (_) {
    return '';
  }
}

export function writeTargetAdapter(name) {
  const value = typeof name === 'string' ? name.trim() : '';
  if (!value) return;
  try {
    localStorage.setItem(TARGET_ADAPTER_KEY, value);
  } catch (_) {
    // 存储不可用时仅保留内存中的选择
  }
}

export function readDns64Backups() {
  try {
    const raw = localStorage.getItem(DNS64_BACKUP_KEY);
    const parsed = raw ? JSON.parse(raw) : null;
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
  } catch (_) {
    return {};
  }
}

export function writeDns64Backups(map) {
  try {
    localStorage.setItem(DNS64_BACKUP_KEY, JSON.stringify(map));
  } catch (_) {
    // 存储不可用时仅保留内存中的备份
  }
}

export function rememberDns64Backup(adapterName, servers) {
  if (!adapterName || !Array.isArray(servers) || servers.length === 0) return;
  const map = readDns64Backups();
  map[adapterName] = servers;
  writeDns64Backups(map);
}

export function forgetDns64Backup(adapterName) {
  const map = readDns64Backups();
  if (!adapterName || !(adapterName in map)) return;
  delete map[adapterName];
  writeDns64Backups(map);
}

export function recallDns64Backup(adapterName) {
  if (dns64ModeState.backupIpv6Servers.length > 0) {
    return dns64ModeState.backupIpv6Servers;
  }
  const stored = readDns64Backups()[adapterName];
  return Array.isArray(stored) ? stored : [];
}
