// 时间与剪贴板工具

/** 相对时间：刚刚 / x 分钟前 / x 小时前 / x 天前 / 具体日期 */
export function timeAgo(ts) {
  if (!ts) return '从未使用'
  const diff = Date.now() - ts
  if (diff < 60 * 1000) return '刚刚'
  if (diff < 3600 * 1000) return `${Math.floor(diff / 60000)} 分钟前`
  if (diff < 24 * 3600 * 1000) return `${Math.floor(diff / 3600000)} 小时前`
  if (diff < 30 * 24 * 3600 * 1000) return `${Math.floor(diff / 86400000)} 天前`
  const d = new Date(ts)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(
    d.getDate()
  ).padStart(2, '0')}`
}

/** 完整日期时间 */
export function fmtDateTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(
    d.getHours()
  )}:${p(d.getMinutes())}`
}

/** 复制文本到剪贴板，返回是否成功（任何情况下不抛异常） */
export async function copyText(text) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      // 按设置在 N 秒后自动清空剪贴板（防敏感信息残留）
      const clearSec = parseInt(localStorage.getItem('ky_clipboard_clear') || '0', 10)
      if (clearSec > 0) {
        setTimeout(() => {
          navigator.clipboard?.writeText('').catch(() => {})
        }, clearSec * 1000)
      }
      return true
    }
  } catch {
    /* 降级到 execCommand */
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}
