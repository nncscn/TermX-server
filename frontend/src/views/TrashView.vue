<script setup>
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { purgeKey, restoreKey } from '@/api/keys'
import { purgeCredential, restoreCredential } from '@/api/credentials'
import { apiTrashEmpty, apiTrashList } from '@/api/trash'
import { timeAgo } from '@/utils/format'
import { getTrashRetention } from '@/utils/retention'

const loading = ref(true)
// 统一形态的回收站条目
const items = ref([])

const bannerDesc = computed(() => {
  const d = getTrashRetention()
  return d
    ? `删除的条目保留 ${d} 天后自动彻底删除，期间可随时恢复`
    : '删除的密钥与凭据会在这里保留，可随时恢复或彻底删除'
})

// 分页：每页条数自适应可视高度（与凭据页一致）
const page = ref(1)
const pageSize = ref(12)

const pageSizeOptions = [12, 24, 36]
function fitRows() {
  const card = document.querySelector('.table-card')
  if (!card) return
  const wrap =
    card.querySelector('.el-scrollbar__wrap') || card.querySelector('.el-table__body-wrapper')
  const rows = [...card.querySelectorAll('.el-table__row')]
  if (!wrap || !rows.length) return
  card.style.setProperty('--ky-row-pad', '8px')
  const total = rows.reduce((s, r) => s + r.offsetHeight, 0)
  const deficit = total - wrap.clientHeight
  if (deficit > 0) {
    const reducePerSide = Math.min(6, deficit / rows.length / 2)
    let pad = Math.max(2, 8 - Math.ceil(reducePerSide))
    card.style.setProperty('--ky-row-pad', pad + 'px')
    const total2 = rows.reduce((s, r) => s + r.offsetHeight, 0)
    if (total2 - wrap.clientHeight > 2 && pad > 2) {
      card.style.setProperty('--ky-row-pad', pad - 1 + 'px')
    }
  }
}
let resizeTimer = null
const onResize = () => {
  clearTimeout(resizeTimer)
  resizeTimer = setTimeout(fitRows, 200)
}
const pagedItems = computed(() =>
  items.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value)
)


async function refresh() {
  loading.value = true
  try {
    items.value = await apiTrashList()
  } catch (e) {
    items.value = []
    ElMessage.error(e.message || '加载回收站失败')
  } finally {
    // 数据减少后收敛页码，避免停留在空页
    const maxPage = Math.max(1, Math.ceil(items.value.length / pageSize.value))
    if (page.value > maxPage) page.value = maxPage
    loading.value = false
  }
}

onMounted(async () => {
  window.addEventListener('resize', onResize)
  await refresh()
  nextTick(fitRows)
})
onUnmounted(() => {
  window.removeEventListener('resize', onResize)
  clearTimeout(resizeTimer)
})

async function onRestore(item) {
  if (item.kind === 'key') {
    await restoreKey(item.id)
  } else {
    await restoreCredential(item.id)
  }
  ElMessage.success(`「${item.name}」已恢复`)
  await refresh()
}

async function onPurge(item) {
  try {
    await ElMessageBox.confirm(
      `彻底删除后无法恢复，确定删除「${item.name}」？`,
      '彻底删除',
      { type: 'warning', confirmButtonText: '彻底删除', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' }
    )
  } catch {
    return
  }
  if (item.kind === 'key') {
    await purgeKey(item.id)
  } else {
    await purgeCredential(item.id)
  }
  ElMessage.success('已彻底删除')
  await refresh()
}

async function onClearAll() {
  if (!items.value.length) return
  try {
    await ElMessageBox.confirm(
      `回收站共 ${items.value.length} 条记录，全部彻底删除后无法恢复。`,
      '清空回收站',
      { type: 'warning', confirmButtonText: '全部删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  try {
    await apiTrashEmpty()
    page.value = 1
    ElMessage.success('回收站已清空')
  } catch (e) {
    ElMessage.error(e.message || '清空失败')
  }
  await refresh()
}
</script>

<template>
  <div class="page stagger">
    <!-- 信息条卡片 -->
    <div class="trash-banner card">
      <div class="banner-left">
        <div class="banner-icon"><el-icon :size="17"><InfoFilled /></el-icon></div>
        <div>
          <div class="banner-title">回收站 · {{ items.length }} 条记录</div>
          <div class="banner-desc">{{ bannerDesc }}</div>
        </div>
      </div>
      <el-button
        type="danger"
        plain
        round
        :disabled="!items.length"
        @click="onClearAll"
      >
        <el-icon style="margin-right: 5px"><Delete /></el-icon>
        清空回收站
      </el-button>
    </div>

    <div class="card table-card" v-loading="loading">
      <el-table :data="pagedItems" style="width: 100%">
        <el-table-column label="类型" width="100">
          <template #default="{ row }">
            <el-tag v-if="row.kind === 'key'" type="primary" effect="light" size="small" round>
              密钥
            </el-tag>
            <el-tag v-else type="success" effect="light" size="small" round>凭据</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="名称" min-width="200">
          <template #default="{ row }">
            <div class="name-cell">
              <span class="name">{{ row.name }}</span>
              <code class="kind-sub">{{ row.kind === 'key' ? 'SSH 密钥' : '连接凭据' }}</code>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="摘要" min-width="220">
          <template #default="{ row }">
            <code class="sum">{{ row.summary }}</code>
          </template>
        </el-table-column>
        <el-table-column label="删除时间" width="130">
          <template #default="{ row }">
            <span class="muted">{{ timeAgo(row.deletedAt) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110" align="right">
          <template #default="{ row }">
            <el-tooltip content="恢复到原列表" placement="top">
              <button type="button" class="icon-op" @click="onRestore(row)">
                <el-icon><RefreshLeft /></el-icon>
              </button>
            </el-tooltip>
            <span class="op-divider"></span>
            <el-tooltip content="彻底删除（不可恢复）" placement="top">
              <button type="button" class="icon-op danger" @click="onPurge(row)">
                <el-icon><Delete /></el-icon>
              </button>
            </el-tooltip>
          </template>
        </el-table-column>
        <template #empty>
          <div class="ky-empty">
            <div class="empty-icon"><el-icon :size="26"><Delete /></el-icon></div>
            <div class="empty-title">回收站是空的</div>
            <div class="empty-desc">删除的密钥和凭据会出现在这里，保留至你彻底删除</div>
          </div>
        </template>
      </el-table>
      <div class="pager-row">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="items.length"
          :page-sizes="pageSizeOptions"
          layout="total, sizes, prev, pager, next"
          background
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
.trash-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  padding: 14px 18px;
  margin-bottom: 16px;
}
/* 回收站顶部多一张信息条卡（约 92px）：表格卡扣除后占满剩余视口 */
.table-card {
  height: calc(100vh - 268px);
}
.banner-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.banner-icon {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 10px;
  background: #eef4fd;
  color: var(--ky-primary);
}
.banner-title {
  font-size: 14px;
  font-weight: 700;
}
.banner-desc {
  margin-top: 2px;
  font-size: 12.5px;
  color: var(--ky-text-faint);
}
.name {
  font-weight: 600;
}
.sum {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  color: #6b7a90;
}
.muted {
  color: var(--ky-text-faint);
  font-size: 12.5px;
}
.name-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 2px;
  padding: 9px 0;
}
.kind-sub {
  font-size: 11.5px;
  color: var(--ky-text-faint);
  letter-spacing: 0.3px;
}
.pager-row {
  display: flex;
  justify-content: flex-end;
  padding: 14px 16px 6px;
  border-top: 1px solid #f0f3f9;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: auto;
}
@media (max-width: 640px) {
  .table-card {
    height: auto;
    min-height: 420px;
  }
}
</style>
