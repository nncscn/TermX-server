import { request } from './client'

/** 从回收站恢复历史密钥条目 */
export function restoreKey(id) {
  return request(`/api/v1/keys/${id}/restore`, { method: 'POST' })
}

/** 彻底删除历史密钥条目 */
export function purgeKey(id) {
  return request(`/api/v1/keys/${id}/purge`, { method: 'POST' })
}
