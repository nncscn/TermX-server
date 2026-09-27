// 服务端蛇形命名与密文字段在两个方向上分别转换。
import { request } from './client'

const toMs = (t) => (t ? new Date(t).getTime() : null)
const toIso = (ms) => (ms == null ? null : new Date(ms).toISOString())

/** 验主密码导出全量数据（含密文层解密内容与回收站条目），返回备份文件数据形态 */
export async function apiExportData(password) {
  const d = await request('/api/v1/data/export', { method: 'POST', body: { password } })
  return {
    keys: (d.keys || []).map((k) => ({
      id: k.id,
      name: k.name,
      type: k.type,
      fingerprint: k.fingerprint || '',
      publicKey: k.public_key || '',
      privateKey: k.private_key || '',
      keyPassphrase: k.passphrase || '',
      remark: k.remark || '',
      createdAt: toMs(k.created_at),
      updatedAt: toMs(k.updated_at),
      deletedAt: toMs(k.deleted_at)
    })),
    credentials: (d.credentials || []).map((c) => ({
      id: c.id,
      name: c.name,
      protocol: c.protocol || 'ssh',
      authType: c.auth_type || 'password',
      group: c.group || '',
      keyId: c.key_id != null ? c.key_id : '',
      host: c.host || '',
      port: c.port ?? null,
      username: c.username || '',
      password: c.password || '',
      proxy: { host: '', username: '', password: '', ...(c.proxy || {}) },
      serial: c.serial
        ? {
            baudRate: c.serial.baud_rate,
            dataBits: c.serial.data_bits,
            stopBits: c.serial.stop_bits,
            parity: c.serial.parity
          }
        : null,
      remark: c.remark || '',
      lastUsedAt: toMs(c.last_used_at),
      createdAt: toMs(c.created_at),
      updatedAt: toMs(c.updated_at),
      deletedAt: toMs(c.deleted_at)
    }))
  }
}

/** 验主密码导入备份（整体替换当前数据；凭据 keyId 引用包内密钥 id，服务端重映射） */
export function apiImportData(password, backup) {
  const keys = (backup.keys || []).map((k) => ({
    id: k.id,
    name: k.name,
    type: k.type,
    fingerprint: k.fingerprint || '',
    public_key: k.publicKey || '',
    private_key: k.privateKey || '',
    passphrase: k.keyPassphrase || '',
    remark: k.remark || '',
    created_at: toIso(k.createdAt),
    updated_at: toIso(k.updatedAt),
    deleted_at: toIso(k.deletedAt)
  }))
  const credentials = (backup.credentials || []).map((c) => ({
    name: c.name,
    protocol: c.protocol || 'ssh',
    auth_type: c.authType || 'password',
    group: c.group || '',
    key_id: (c.authType || 'password') === 'key' && c.keyId ? c.keyId : null,
    host: c.host,
    port: c.port ?? null,
    username: c.username || '',
    password: c.password || '',
    proxy: c.proxy || {},
    serial: c.serial
      ? {
          baud_rate: c.serial.baudRate,
          data_bits: c.serial.dataBits,
          stop_bits: c.serial.stopBits,
          parity: c.serial.parity
        }
      : null,
    remark: c.remark || '',
    last_used_at: toIso(c.lastUsedAt),
    created_at: toIso(c.createdAt),
    updated_at: toIso(c.updatedAt),
    deleted_at: toIso(c.deletedAt)
  }))
  return request('/api/v1/data/import', {
    method: 'POST',
    body: { password, keys, credentials }
  })
}
