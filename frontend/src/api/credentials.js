// 备注/最后使用），连接信息（主机/端口/用户名/密码/代理/串口参数）服务端
import { request } from './client'
import { decryptData, encryptData, getSessionDataKey } from '@/utils/crypto'

/** 支持的协议与默认端口（serial 无端口） */
export const PROTOCOLS = [
  { value: 'ssh', label: 'SSH', port: 22 },
  { value: 'rdp', label: 'RDP', port: 3389 },
  { value: 'vnc', label: 'VNC', port: 5900 },
  { value: 'telnet', label: 'Telnet', port: 23 },
  { value: 'serial', label: 'Serial', port: null }
]

export const defaultPort = (protocol) =>
  PROTOCOLS.find((p) => p.value === protocol)?.port ?? 22

/** 按协议生成可复制的连接地址（reveal 解密后才有完整内容） */
export function addressText(c) {
  if (!c) return ''
  if (c.protocol === 'ssh')
    return c.port === 22
      ? `ssh ${c.username}@${c.host}`
      : `ssh ${c.username}@${c.host} -p ${c.port}`
  if (c.protocol === 'telnet') return `telnet ${c.host} ${c.port ?? ''}`.trim()
  if (c.protocol === 'serial') return c.host
  return `${c.protocol}://${c.username}@${c.host}${c.port ? ':' + c.port : ''}`
}

const toMs = (t) => (t ? new Date(t).getTime() : null)

/** 服务端条目 → 页面数据形态（camelCase + 毫秒时间戳） */
export function mapCred(c) {
  return {
    id: c.id,
    name: c.name,
    protocol: c.protocol || 'ssh',
    host: '',
    port: null,
    username: '',
    password: '',
    keyContent: '',
    secretBlob: c.secret_blob || '', // 客户端密文（Base64），编辑时本地解密
    // 编辑弹窗降级为"仅基本信息"模式
    isTermx: isTermxBlob(c.secret_blob || ''),
    authType: c.auth_type || 'password',
    keyId: c.key_id != null ? c.key_id : '',
    group: c.group || '',
    proxy: {},
    serial: null,
    remark: c.remark || '',
    lastUsedAt: toMs(c.last_used_at),
    createdAt: toMs(c.created_at),
    updatedAt: toMs(c.updated_at),
    deletedAt: toMs(c.deleted_at)
  }
}

/** 判断密文是否为 TermX 同步的端到端密文（JSON 包装 {"v":1,"c","n"}；
 *  本 Web 端的密文是二进制 GCM blob，JSON 解析出 v=1 即为 TermX 条目） */
export function isTermxBlob(blobBase64) {
  if (!blobBase64) return false
  try {
    const d = JSON.parse(atob(blobBase64))
    return !!d && d.v === 1 && typeof d.c === 'string' && typeof d.n === 'string'
  } catch {
    return false
  }
}

const serialToServer = (s) =>
  s
    ? { baud_rate: s.baudRate, data_bits: s.dataBits, stop_bits: s.stopBits, parity: s.parity }
    : null

/** 创建/更新请求体：明文层 + 客户端加密密文层（key_id 仅密钥认证时下发）。
 *  metadataOnly（仅 TermX 同步条目的元数据编辑）：不带 secret_blob，
 *  服务端保留原密文 */
async function saveBody(d, metadataOnly = false) {
  if (metadataOnly) {
    return {
      name: d.name,
      protocol: d.protocol || 'ssh',
      auth_type: d.authType || 'password',
      group: d.group || '',
      key_id: (d.authType || 'password') === 'key' && d.keyId ? d.keyId : null,
      remark: d.remark || ''
    }
  }
  const secret = {
    host: d.host,
    port: d.port ?? defaultPort(d.protocol),
    username: d.username || '',
    password: d.password || '',
    key_content: d.keyContent || '',
    proxy: d.proxy || {},
    serial: (d.protocol || 'ssh') === 'serial' ? serialToServer(d.serial) : null
  }
  return {
    name: d.name,
    protocol: d.protocol || 'ssh',
    auth_type: d.authType || 'password',
    group: d.group || '',
    key_id: (d.authType || 'password') === 'key' && d.keyId ? d.keyId : null,
    remark: d.remark || '',
    secret_blob: encryptData(getSessionDataKey(), secret)
  }
}

/**
 * 查询凭据列表
 * @param {{keyword?: string, authType?: string, group?: string, includeDeleted?: boolean}} opts
 */
export async function listCredentials(opts = {}) {
  const params = new URLSearchParams()
  if (opts.keyword) params.set('keyword', opts.keyword)
  if (opts.authType) params.set('auth_type', opts.authType)
  if (opts.group) params.set('group', opts.group)
  if (opts.includeDeleted) params.set('include_deleted', 'true')
  const qs = params.toString()
  const data = await request('/api/v1/credentials' + (qs ? `?${qs}` : ''))
  return (data.list || []).map(mapCred)
}

export async function createCredential(d) {
  return mapCred(await request('/api/v1/credentials', { method: 'POST', body: await saveBody(d) }))
}

export async function updateCredential(id, d, opts = {}) {
  return mapCred(
    await request(`/api/v1/credentials/${id}`, {
      method: 'PUT',
      body: await saveBody(d, !!opts.metadataOnly)
    })
  )
}

/** 删除（进入回收站） */
export function removeCredential(id) {
  return request(`/api/v1/credentials/${id}`, { method: 'DELETE' })
}

/** 从回收站恢复 */
export function restoreCredential(id) {
  return request(`/api/v1/credentials/${id}/restore`, { method: 'POST' })
}

/** 彻底删除 */
export function purgeCredential(id) {
  return request(`/api/v1/credentials/${id}/purge`, { method: 'POST' })
}

/** 标记"最后使用"（连接/复制密码等动作触发） */
export function touchUsed(id) {
  return request(`/api/v1/credentials/${id}/touch`, { method: 'POST' })
}

/** 用数据密钥本地解密连接信息（编辑弹窗「输入数据密钥」步骤） */
export function decryptCredSecret(keyInput, blobBase64) {
  const d = decryptData(keyInput, blobBase64)
  return {
    host: d.host || '',
    port: d.port ?? null,
    username: d.username || '',
    password: d.password || '',
    keyContent: d.key_content || '',
    proxy: { host: '', username: '', password: '', ...(d.proxy || {}) },
    serial: d.serial
      ? {
          baudRate: d.serial.baud_rate,
          dataBits: d.serial.data_bits,
          stopBits: d.serial.stop_bits,
          parity: d.serial.parity
        }
      : null
  }
}

/** 汇总所有出现过的分组（供筛选）：由未删除列表派生 */
export async function listAllGroups() {
  const list = await listCredentials()
  return [...new Set(list.map((c) => (c.group || '').trim()).filter(Boolean))]
}
