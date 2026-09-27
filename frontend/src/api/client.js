const TOKEN_KEY = 'ky_token'

/** 当前后端基础地址（scheme + host），如 http://192.168.61.127:8080；'' 表示同源 */
export function apiBase() {
  const raw = (localStorage.getItem('ky_api_base') || '').trim()
  if (!raw) return ''
  const scheme = (localStorage.getItem('ky_api_scheme') || 'http') === 'https' ? 'https' : 'http'
  const host = raw.replace(/^https?:\/\//, '')
  return `${scheme}://${host}`
}

/** 读取令牌：本地持久 优先于 会话级 */
export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || sessionStorage.getItem(TOKEN_KEY) || ''
}

/** 保存令牌：remember=true 持久保存（关浏览器仍有效），否则仅当前会话 */
export function setToken(token, remember) {
  clearToken()
  if (!token) return
  ;(remember ? localStorage : sessionStorage).setItem(TOKEN_KEY, token)
}

/** 清除令牌（登出 / 重置密码后） */
export function clearToken() {
  localStorage.removeItem(TOKEN_KEY)
  sessionStorage.removeItem(TOKEN_KEY)
}

/**
 * 统一请求：自动拼基础地址、带令牌、解包 {code,message,data}。
 * 业务失败（code!==0）抛 Error(message)，网络/HTTP 异常抛 Error(友好文案)。
 */
export async function request(path, { method = 'GET', body, auth = true } = {}) {
  const headers = { 'Content-Type': 'application/json' }
  if (auth) {
    const token = getToken()
    if (token) headers.Authorization = `Bearer ${token}`
  }
  let resp
  try {
    resp = await fetch(`${apiBase()}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined
    })
  } catch {
    throw new Error('无法连接服务器，请检查服务连接设置')
  }
  let payload
  try {
    payload = await resp.json()
  } catch {
    throw new Error(`服务器响应异常（HTTP ${resp.status}）`)
  }
  if (payload.code !== 0) {
    // 错误同为 40101）与主密码输错（40102）不在此列。
    if (payload.code === 40101 && auth) {
      clearToken()
      window.location.href = '/login?expired=1'
    }
    // 错误对象附带业务码与数据（如库名冲突的建议名），供调用方分支处理
    const err = new Error(payload.message || '请求失败')
    err.code = payload.code
    err.data = payload.data
    throw err
  }
  return payload.data
}
