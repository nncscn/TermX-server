import { request } from './client'

/** 回收站列表（kind: 'key' 密钥 / 'cred' 凭据） */
export async function apiTrashList() {
  const d = await request('/api/v1/trash')
  return (d.list || []).map((t) => ({
    kind: t.kind === 'key' ? 'key' : 'cred',
    id: t.id,
    name: t.name,
    summary: t.summary,
    deletedAt: t.deleted_at ? new Date(t.deleted_at).getTime() : null
  }))
}

/** 清空回收站，返回 {purged} */
export function apiTrashEmpty() {
  return request('/api/v1/trash/empty', { method: 'POST' })
}

/** 清理超过保留天数的条目（回收站自动清理），返回 {purged} */
export function apiTrashClean(days) {
  return request('/api/v1/trash/clean', { method: 'POST', body: { days } })
}
