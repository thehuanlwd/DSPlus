/* ── DSPlus 国际化模块（易扩展语言系统） ──
 * 用法：
 *   await initI18n();           // 启动时调用
 *   const txt = t('nav.dashboard');
 *   applyI18n();                // 批量应用 data-i18n
 *   await setLanguage('en');    // 切换语言（会立即刷新 UI）
 *
 * 添加新语言：
 *   1. 在 web/locales/ 下新增 xx.json
 *   2. 提供 "_displayName"
 *   3. 前端下拉可后续改为动态读取 /locales 目录（当前为保证简单先写死两个选项）
 */

let i18nCurrentLang = 'zh';
let i18nTranslations = {};

/**
 * 简单插值：支持 t('key', {name: 'foo'}) → 替换 {{name}}
 */
function interpolate(str, params = {}) {
  if (!str || typeof str !== 'string') return str || '';
  return str.replace(/\{\{\s*(\w+)\s*\}\}/g, (_, k) => {
    return (params[k] !== undefined) ? params[k] : `{{${k}}}`;
  });
}

/**
 * 翻译函数（核心）
 */
function t(key, params = {}) {
  const val = i18nTranslations[key];
  if (val == null) {
    // 开发时容易发现缺失 key
    if (window.location.hostname === '127.0.0.1' || window.location.hostname === 'localhost') {
      console.warn('[i18n] missing key:', key);
    }
    return key; // 兜底返回 key
  }
  return interpolate(val, params);
}

/**
 * 从服务端加载指定语言的翻译文件
 */
async function loadLocale(lang) {
  try {
    const res = await fetch(`/locales/${lang}.json`, { cache: 'no-store' });
    if (!res.ok) throw new Error('load failed');
    return await res.json();
  } catch (e) {
    console.warn('[i18n] failed to load locale', lang, e);
    // 回退到中文
    if (lang !== 'zh') {
      return loadLocale('zh');
    }
    return {};
  }
}

/**
 * 初始化 i18n（读取 config 中的 language 并加载）
 */
async function initI18n() {
  try {
    const r = await fetch('/api/config');
    const cfg = await r.json();
    const lang = cfg.language || 'zh';
    await setLanguage(lang, false); // 不触发保存
  } catch (e) {
    // 配置加载失败时使用默认
    await setLanguage('zh', false);
  }
}

/**
 * 切换语言
 * applyNow = true 时会立即调用 applyI18n() 并尝试通知业务层重渲染
 */
async function setLanguage(lang, applyNow = true) {
  if (!lang) lang = 'zh';
  const data = await loadLocale(lang);
  i18nTranslations = data || {};
  i18nCurrentLang = lang;

  // 更新 <html lang>
  document.documentElement.lang = (lang === 'zh') ? 'zh-CN' : 'en';

  if (applyNow) {
    applyI18n();

    // 通知业务层重新渲染动态内容
    if (typeof window.refreshI18nDynamic === 'function') {
      window.refreshI18nDynamic();
    }
  }
}

/**
 * 批量应用翻译到 DOM
 * 使用 data-i18n="key" 替换 textContent
 * data-i18n-placeholder / data-i18n-title 等用于属性
 */
function applyI18n(root = document) {
  // 文本内容
  root.querySelectorAll('[data-i18n]').forEach(el => {
    const key = el.getAttribute('data-i18n');
    if (key) {
      el.textContent = t(key);
    }
  });

  // placeholder
  root.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
    const key = el.getAttribute('data-i18n-placeholder');
    if (key) el.setAttribute('placeholder', t(key));
  });

  // title
  root.querySelectorAll('[data-i18n-title]').forEach(el => {
    const key = el.getAttribute('data-i18n-title');
    if (key) el.setAttribute('title', t(key));
  });

  // 其他常见属性可按需扩展
}

/**
 * 获取当前语言代码
 */
function getCurrentLang() {
  return i18nCurrentLang;
}

// 暴露给全局（方便其他脚本直接使用 t / setLanguage）
window.t = t;
window.setLanguage = setLanguage;
window.applyI18n = applyI18n;
window.getCurrentLang = getCurrentLang;

// 也导出模块风格（如果未来用 import）
if (typeof module !== 'undefined') {
  module.exports = { t, initI18n, setLanguage, applyI18n, getCurrentLang };
}
