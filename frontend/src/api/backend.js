import { clearToken, request, setToken } from './client'

/** 登录：成功保存令牌并返回 data（token/expires_at） */
export async function apiLogin(username, password, remember, captcha = null) {
  const body = { username, password }
  if (captcha) {
    body.captcha_id = captcha.captchaId
    body.captcha_clicks = captcha.clicks
  }
  const data = await request('/api/v1/auth/login', {
    method: 'POST',
    body,
    auth: false
  })
  setToken(data.token, remember)
  return data
}

/** 获取汉字点选验证码（id + SVG 图 + 提示语） */
export function apiGetCaptcha() {
  return request('/api/v1/auth/captcha', { auth: false })
}

/** 登出：删除服务端会话并清除本地令牌 */
export async function apiLogout() {
  try {
    await request('/api/v1/auth/logout', { method: 'POST' })
  } finally {
    clearToken()
  }
}

/** 修改主密码（需登录） */
export function apiChangePassword(oldPassword, newPassword) {
  return request('/api/v1/auth/password/change', {
    method: 'POST',
    body: { old_password: oldPassword, new_password: newPassword }
  })
}

/** 生成/重置恢复密钥（需登录并验主密码——服务端要解包库密钥重包恢复层；明文仅返回一次） */
export function apiGenerateRecoveryKey(password) {
  return request('/api/v1/auth/recovery/generate', {
    method: 'POST',
    body: { password }
  })
}

/** 忘记密码第一步：验证恢复密钥 → 返回一次性重置凭证 */
export function apiForgotVerify(username, recoveryKey) {
  return request('/api/v1/auth/forgot/verify', {
    method: 'POST',
    body: { username, recovery_key: recoveryKey },
    auth: false
  })
}

/** 忘记密码第二步：凭重置凭证 + 恢复密钥设置新密码（恢复密钥用于解包库密钥，数据不丢；成功后所有会话失效） */
export function apiForgotReset(resetTicket, recoveryKey, newPassword) {
  return request('/api/v1/auth/forgot/reset', {
    method: 'POST',
    body: { reset_ticket: resetTicket, recovery_key: recoveryKey, new_password: newPassword },
    auth: false
  })
}
