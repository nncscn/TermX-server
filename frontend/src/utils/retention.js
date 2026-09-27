import { apiTrashClean } from '@/api/trash'

/** 当前保留天数（0 = 关闭自动清理） */
export function getTrashRetention() {
  return parseInt(localStorage.getItem('ky_trash_retention') || '0', 10)
}

export function setTrashRetention(days) {
  localStorage.setItem('ky_trash_retention', String(days))
}

/** 清理当前账户回收站中超过保留期的条目（服务端执行），返回清理数量 */
export async function applyTrashRetention() {
  const days = getTrashRetention()
  if (!days) return 0
  try {
    const r = await apiTrashClean(days)
    return r?.purged || 0
  } catch {
    return 0 // 未登录/网络异常时静默跳过，不打断页面
  }
}
