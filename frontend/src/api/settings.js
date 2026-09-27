// 设置 API：账户偏好存数据库（每账户一行）；数据库连接配置读写服务端 config.yaml。
// 继续读本地键，数据库是事实源，本地只是运行缓存。
import { request } from './client'

/** 把服务端偏好同步到本地缓存键 */
export function syncPrefsCache(p) {
  if (!p) return
  localStorage.setItem('ky_start_page', p.start_page || '/dashboard')
  localStorage.setItem('ky_clipboard_clear', String(p.clipboard_clear ?? 0))
  localStorage.setItem('ky_autolock', String(p.autolock_minutes ?? 0))
  localStorage.setItem('ky_trash_retention', String(p.trash_retention_days ?? 0))
}

/** 读取偏好（首访自动落默认行）并同步本地缓存 */
export async function apiGetSettings() {
  const p = await request('/api/v1/settings')
  syncPrefsCache(p)
  return p
}

/** 保存偏好（整体提交四项）并同步本地缓存 */
export async function apiUpdateSettings(payload) {
  const p = await request('/api/v1/settings', { method: 'PUT', body: payload })
  syncPrefsCache(p)
  return p
}

/** 读取数据库连接配置（分字段回显；密码不回显，has_password 标记是否已设置） */
export function apiGetDbConfig() {
  return request('/api/v1/settings/database')
}

/**
 * 真实连通测试（不落盘）。
 * @param {{engine, host?, port?, user?, password?, name?, file?}} cfg
 */
export function apiTestDb(cfg) {
  return request('/api/v1/settings/database/test', { method: 'POST', body: cfg })
}

/**
 * 保存数据库配置并自动切换：服务端先验主密码、自动建库（外部引擎）、
 * 真实连通测试，通过后备份 config.yaml 重写并自动重启后端。
 * Password 留空表示沿用当前配置中的密码；master_password 为登录主密码。
 */
export function apiSaveDb(cfg) {
  return request('/api/v1/settings/database', { method: 'PUT', body: cfg })
}

/** 撤销待生效的数据库变更（恢复为当前运行配置，不重启） */
export function apiRevertDb(masterPassword) {
  return request('/api/v1/settings/database/revert', {
    method: 'POST',
    body: { master_password: masterPassword }
  })
}

/** 检测服务器本机端口占用（设置页·端口占用检测） */
export function apiCheckPort(port) {
  return request('/api/v1/settings/server/check-port', {
    method: 'POST',
    body: { port: Number(port) }
  })
}
