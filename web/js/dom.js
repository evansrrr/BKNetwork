import { dns64ModeState } from './state.js';

export const logEl = document.getElementById('log');
export const advancedModeToggleEl = document.getElementById('advancedModeToggle');
export const backendBadgeEl = document.getElementById('backendBadge');
export const backendBadgeDotEl = document.getElementById('backendDot');
export const adminBadgeEl = document.getElementById('adminBadge');
export const adminBadgeDotEl = document.getElementById('adminDot');
export const networkBadgeEl = document.getElementById('networkBadge');
export const networkBadgeDotEl = document.getElementById('networkDot');
export const freeBadgeEl = document.getElementById('freeBadge');
export const freeBadgeDotEl = document.getElementById('freeDot');
export const ipv6CheckDotEl = document.getElementById('ipv6CheckDot');
export const ipv6CheckTextEl = document.getElementById('ipv6CheckText');
export const ipv6RefreshBtnEl = document.getElementById('ipv6RefreshBtn');
export const warpToggleEl = document.getElementById('warpToggle');
export const warpStateEl = document.getElementById('warpState');
export const warpSettingsStateEl = document.getElementById('warpSettingsState');
export const warpSettingsModeEl = document.getElementById('warpSettingsMode');
export const warpSettingsTunnelProtocolEl = document.getElementById('warpSettingsTunnelProtocol');
export const easyModeToggleEl = document.getElementById('easyModeToggle');
export const easyModeStateEl = document.getElementById('easyModeState');
export const dns64ModeToggleEl = document.getElementById('dns64ModeToggle');
export const dns64ModeStateEl = document.getElementById('dns64ModeState');
export const settingsOpenBtn = document.getElementById('settingsOpenBtn');
export const settingsOverlayEl = document.getElementById('settingsOverlay');
export const settingsCloseBtn = document.getElementById('settingsCloseBtn');
export const settingsDiagnosticsBtn = document.getElementById('settingsDiagnosticsBtn');
export const settingAutoStartEl = document.getElementById('settingAutoStart');
export const settingSilentStartEl = document.getElementById('settingSilentStart');
export const settingWarpAppAutoStartEl = document.getElementById('settingWarpAppAutoStart');
export const settingDisableOnlineChecksEl = document.getElementById('settingDisableOnlineChecks');
export const operationToastEl = document.getElementById('operationToast');
export const operationToastTitleEl = document.getElementById('operationToastTitle');
export const operationToastDescEl = document.getElementById('operationToastDesc');
export const settingsStatusEl = document.getElementById('settingsStatus');
export const stackModeStateEl = document.getElementById('stackModeState');
export const dnsCardEl = document.getElementById('dnsCard');
export const appVersionEl = document.querySelector('.lead');
export const dnsIpv4InputEl = document.getElementById('dnsIpv4Input');
export const dnsIpv6InputEl = document.getElementById('dnsIpv6Input');
export const dnsStatusEl = document.getElementById('dnsStatus');
export const lastUpdatedEl = document.getElementById('lastUpdated');
export const trafficUsageEl = document.getElementById('trafficUsage');
export const adapterListEl = document.getElementById('adapterList');
export const targetAdapterSelects = ['ifName', 'ifName2'].map(id => document.getElementById(id));
export const stackModeButtons = {
  ipv4: document.getElementById('btnIpv4'),
  ipv6: document.getElementById('btnIpv6'),
  both: document.getElementById('btnBoth'),
};
export const buttons = [stackModeButtons.ipv4, stackModeButtons.ipv6, stackModeButtons.both, warpToggleEl, easyModeToggleEl, dns64ModeToggleEl].filter(Boolean);
