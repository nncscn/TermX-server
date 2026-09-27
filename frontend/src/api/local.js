
// ---------- 记住的账户名（"记住我"勾选时保存，用于下次预填） ----------
const REMEMBER_KEY = 'ky_remember_name'

export function getRememberedName() {
  return localStorage.getItem(REMEMBER_KEY) || ''
}

export function setRememberedName(name) {
  if (name) localStorage.setItem(REMEMBER_KEY, name)
  else localStorage.removeItem(REMEMBER_KEY)
}

// ---------- 登录失败计数与冷却提示（与后端阈值一致的前端提示档） ----------
const FAILS_KEY = 'ky_login_fails'
export const CAPTCHA_AFTER = 5 // 连续失败 5 次后需要图形验证码
const COOLDOWN_AFTER = 10 // 连续失败 10 次进入冷却
const COOLDOWN_MS = 60 * 1000

function loadFails() {
  try {
    return JSON.parse(localStorage.getItem(FAILS_KEY)) || {}
  } catch {
    return {}
  }
}

/**
 * 某账户名的失败状态。按"输入的名字"计数（不区分账户是否存在，
 * 行为完全一致，防止枚举探测）。冷却结束后计数回落到验证码档。
 */
export function getLoginFails(name) {
  const map = loadFails()
  const st = { count: 0, cooldownUntil: 0, ...(map[name] || {}) }
  if (st.cooldownUntil && Date.now() > st.cooldownUntil) {
    st.count = CAPTCHA_AFTER
    st.cooldownUntil = 0
    map[name] = st
    localStorage.setItem(FAILS_KEY, JSON.stringify(map))
  }
  return { ...st, needCaptcha: st.count >= CAPTCHA_AFTER }
}

/** 记录一次失败；达到阈值时返回冷却截止时间戳 */
export function recordLoginFail(name) {
  const map = loadFails()
  const st = { count: 0, cooldownUntil: 0, ...(map[name] || {}) }
  st.count += 1
  if (st.count >= COOLDOWN_AFTER && !st.cooldownUntil) {
    st.cooldownUntil = Date.now() + COOLDOWN_MS
  }
  map[name] = st
  localStorage.setItem(FAILS_KEY, JSON.stringify(map))
  return { count: st.count, cooldownUntil: st.cooldownUntil }
}

/** 登录成功后清零失败计数 */
export function clearLoginFails(name) {
  const map = loadFails()
  delete map[name]
  localStorage.setItem(FAILS_KEY, JSON.stringify(map))
}
