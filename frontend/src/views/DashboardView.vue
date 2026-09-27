// FIXME: 图表在窄屏下有溢出问题
<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { PieChart } from 'echarts/charts'
import { LegendComponent, TitleComponent, TooltipComponent } from 'echarts/components'
import VChart from 'vue-echarts'
import { listCredentials } from '@/api/credentials'
import { timeAgo } from '@/utils/format'

use([CanvasRenderer, PieChart, LegendComponent, TitleComponent, TooltipComponent])

const router = useRouter()

const loading = ref(true)
const creds = ref([])

onMounted(async () => {
  ;[creds.value] = await Promise.all([listCredentials()])
  if (!creds.value.length) {
    creds.value = demoCreds()
  }
  loading.value = false
})

function demoCreds() {
  const rows = [
    { name: '生产 Web 服务器', protocol: 'ssh', group: '生产', authType: 'key', lastUsedAt: ago(2) },
    { name: '实验室 VNC 工作站', protocol: 'vnc', group: '测试', authType: 'password', lastUsedAt: ago(6) },
    { name: '公司跳板机', protocol: 'ssh', group: '公司', authType: 'password', lastUsedAt: ago(26) },
    { name: '财务 Windows 终端', protocol: 'rdp', group: '公司', authType: 'password', lastUsedAt: ago(30) },
    { name: '测试环境-订单服务', protocol: 'ssh', group: '测试', authType: 'password', lastUsedAt: ago(5) },
    { name: '个人 VPS', protocol: 'ssh', group: '个人', authType: 'key', lastUsedAt: ago(3) },
    { name: '老防火墙 CLI', protocol: 'telnet', group: '公司', authType: 'password', lastUsedAt: ago(50) },
    { name: '机房交换机 Console', protocol: 'serial', group: '生产', authType: 'password', lastUsedAt: ago(64) },
    { name: '家庭 NAS', protocol: 'ssh', group: '个人', authType: 'password', lastUsedAt: ago(24 * 6) },
    { name: 'Git 平台（内部）', protocol: 'ssh', group: '公司', authType: 'key', lastUsedAt: ago(12) }
  ]
  return rows.map((r, i) => ({ id: 'demo_c' + i, ...r, deletedAt: null }))
}

const weeklyUses = computed(
  () =>
    creds.value.filter(
      (c) => c.lastUsedAt && Date.now() - c.lastUsedAt < 7 * 24 * 3600 * 1000
    ).length
)

const dailyUses = computed(
  () => creds.value.filter((c) => c.lastUsedAt && Date.now() - c.lastUsedAt < 24 * 3600 * 1000).length
)
const statCards = computed(() => [
  {
    label: '连接凭据',
    value: creds.value.length,
    sub: `${new Set(creds.value.map((c) => c.protocol || 'ssh')).size} 种协议`,
    icon: 'Connection',
    cls: 'green',
    to: '/credentials'
  },
  {
    label: '密钥认证凭据',
    value: creds.value.filter((c) => c.authType === 'key').length,
    sub: `密码认证 ${creds.value.filter((c) => c.authType !== 'key').length} 条`,
    icon: 'Key',
    cls: 'blue',
    to: '/credentials'
  },
  {
    label: '回收站条目',
    value: trashCount.value,
    sub: trashCount.value ? '可随时恢复' : '回收站是空的',
    icon: 'Delete',
    cls: 'orange',
    to: '/trash'
  },
  {
    label: '近 7 天使用',
    value: weeklyUses.value,
    sub: `24 小时内 ${dailyUses.value} 次`,
    icon: 'Clock',
    cls: 'cyan',
    to: '/credentials'
  }
])

const trashCount = ref(0)
onMounted(async () => {
  const dc = await listCredentials({ includeDeleted: true })
  trashCount.value = dc.filter((c) => c.deletedAt).length
})

// 凭据协议分布环形图
const PROTO_COLORS = {
  ssh: '#1e6fe0',
  rdp: '#f0a84b',
  vnc: '#3ecf8e',
  telnet: '#8a97ab',
  serial: '#38b6d8'
}
const PROTO_LABELS = { ssh: 'SSH', rdp: 'RDP', vnc: 'VNC', telnet: 'Telnet', serial: 'Serial' }
const protoDist = computed(() => {
  const map = {}
  creds.value.forEach((c) => {
    const p = c.protocol || 'ssh'
    map[p] = (map[p] || 0) + 1
  })
  return Object.entries(map)
    .map(([proto, count]) => ({
      proto,
      label: PROTO_LABELS[proto] || proto.toUpperCase(),
      count,
      pct: creds.value.length ? Math.round((count / creds.value.length) * 100) : 0
    }))
    .sort((a, b) => b.count - a.count)
})
const protoOption = computed(() =>
  makeDonutOption({
    items: protoDist.value,
    nameKey: 'label',
    colorOf: (t) => PROTO_COLORS[t.proto] || '#b8c2d0',
    unitWord: '条'
  })
)


// 问候语与日期
const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 11) return '早上好'
  if (h < 13) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
})
const todayStr = computed(() => {
  const d = new Date()
  const week = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()]
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 星期${week}`
})

// ECharts 环形图统一配置工厂（两张分布卡共用同一视觉语言）
function makeDonutOption({ items, nameKey, colorOf, unitWord }) {
  return {
    color: items.map((t) => colorOf(t)),
    tooltip: {
      trigger: 'item',
      formatter: `{b}：{c} ${unitWord}（{d}%）`
    },
    legend: {
      orient: 'vertical',
      right: 0,
      top: 'middle',
      icon: 'circle',
      itemWidth: 10,
      itemHeight: 10,
      itemGap: 18,
      formatter: (name) => {
        const t = items.find((x) => x[nameKey] === name)
        return `${name}  {c|${t?.count ?? 0}} {p|· ${t?.pct ?? 0}%}`
      },
      textStyle: {
        color: '#47536b',
        fontSize: 12.5,
        rich: {
          c: { fontSize: 13, fontWeight: 700, color: '#1f2430', padding: [0, 2, 0, 0] },
          p: { fontSize: 11.5, color: '#98a3b3' }
        }
      }
    },
    series: [
      {
        type: 'pie',
        radius: ['62%', '84%'],
        center: ['32%', '50%'],
        itemStyle: { borderRadius: 6, borderColor: '#fff', borderWidth: 2 },
        emphasis: { scale: true, scaleSize: 5 },
        label: { show: false },
        labelLine: { show: false },
        data: items.map((t) => ({ name: t[nameKey], value: t.count }))
      }
    ]
  }
}

// 认证方式分布（密码/密钥）
const AUTH_COLORS = { password: '#1e6fe0', key: '#3ecf8e' }
const AUTH_LABELS = { password: '密码认证', key: '密钥认证' }
const authDist = computed(() => {
  const map = {}
  creds.value.forEach((c) => {
    const a = c.authType || 'password'
    map[a] = (map[a] || 0) + 1
  })
  return Object.entries(map).map(([auth, count]) => ({
    auth,
    count,
    pct: creds.value.length ? Math.round((count / creds.value.length) * 100) : 0
  }))
})
const authOption = computed(() =>
  makeDonutOption({
    items: authDist.value,
    nameKey: 'label',
    colorOf: (t) => AUTH_COLORS[t.auth] || '#b8c2d0',
    unitWord: '条'
  })
)

// 最近使用
const recentCreds = computed(() =>
  [...creds.value].filter((c) => c.lastUsedAt).sort((a, b) => b.lastUsedAt - a.lastUsedAt)
)

</script>

<template>
  <div class="page stagger">
    <!-- 问候横幅（瘦身版） -->
      <div class="hello-banner">
        <div class="hello-text">
          <div class="hello-main">
            <span class="hello-title">{{ greeting }}，密钥库运行正常</span>
            <span class="hello-date">{{ todayStr }}</span>
          </div>
          <div class="hello-sub">数据在本机加密存储，从未离开这台设备</div>
        </div>
        <div class="hello-chip">
          <el-icon :size="15"><Lock /></el-icon>
          数据本地加密
        </div>
      </div>

      <!-- 统计卡片（可点击跳转） -->
      <div class="stat-grid">
        <div
          v-for="s in statCards"
          :key="s.label"
          class="card stat-card"
          :class="s.cls"
          @click="router.push(s.to)"
        >
          <div class="stat-icon"><el-icon :size="21"><component :is="s.icon" /></el-icon></div>
          <div class="stat-body">
            <div class="stat-label">{{ s.label }}</div>
            <div class="stat-num">{{ s.value }}</div>
            <div class="stat-sub">{{ s.sub }}</div>
          </div>
          <el-icon class="stat-arrow"><ArrowRight /></el-icon>
        </div>
      </div>

      <div class="dash-row">
        <!-- 认证方式分布环形图 -->
        <div class="card card-pad dist-card">
          <div class="block-title-row">
            <div class="block-title">认证方式分布</div>
            <span class="block-sub">按凭据认证方式</span>
          </div>
          <div v-if="creds.length" class="chart-wrap">
            <div class="donut-hold">
              <v-chart class="donut-chart" :option="authOption" autoresize />
              <div class="donut-center">
                <div class="dc-num">{{ creds.filter((c) => c.authType === 'key').length }}</div>
                <div class="dc-label">条密钥认证</div>
              </div>
            </div>
          </div>
          <div v-else class="ky-empty small">
            <div class="empty-icon"><el-icon :size="22"><Key /></el-icon></div>
            <div class="empty-desc">暂无凭据</div>
          </div>
        </div>

        <!-- 凭据协议分布 -->
        <div class="card card-pad dist-card">
          <div class="block-title-row">
            <div class="block-title">凭据协议分布</div>
            <span class="block-sub">按连接协议</span>
          </div>
          <div v-if="creds.length" class="chart-wrap">
            <div class="donut-hold">
              <v-chart class="donut-chart" :option="protoOption" autoresize />
              <div class="donut-center">
                <div class="dc-num">{{ creds.length }}</div>
                <div class="dc-label">条凭据</div>
              </div>
            </div>
          </div>
          <div v-else class="ky-empty small">
            <div class="empty-icon"><el-icon :size="22"><Connection /></el-icon></div>
            <div class="empty-desc">暂无凭据</div>
          </div>
        </div>
      </div>

      <!-- 最近使用轻列表 -->
      <div class="card recent-card">
        <div class="recent-head">
          <div class="block-title-row">
            <div class="block-title">最近使用</div>
            <span class="block-sub">{{ recentCreds.length }} 条记录</span>
          </div>
        </div>
        <div v-if="recentCreds.length" class="recent-list">
          <div v-for="c in recentCreds.slice(0, 5)" :key="c.id" class="recent-row">
            <div class="recent-main">
              <div class="recent-line">
                <span class="recent-name">{{ c.name }}</span>
                <el-tag v-if="c.authType === 'key'" type="primary" effect="light" size="small" round>
                  密钥
                </el-tag>
                <el-tag v-else type="info" effect="light" size="small" round>密码</el-tag>
              </div>
              <div class="recent-meta">
                <span class="meta-proto">{{ (c.protocol || 'ssh').toUpperCase() }}</span>
                <span v-if="c.group" class="meta-dot">·</span>
                <span v-if="c.group">{{ c.group }}</span>
              </div>
            </div>
            <span class="recent-time">{{ timeAgo(c.lastUsedAt) }}</span>
          </div>
        </div>
        <div v-else class="ky-empty small">
          <div class="empty-icon"><el-icon :size="22"><Clock /></el-icon></div>
          <div class="empty-desc">还没有使用记录，去凭据页复制一条试试</div>
        </div>
        <div class="recent-foot" @click="router.push('/credentials')">
          查看全部凭据
          <el-icon :size="13"><ArrowRight /></el-icon>
        </div>
      </div>
  </div>
</template>

<style scoped>
/* 问候横幅（瘦身版 84px） */
.hello-banner {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 18px 24px;
  margin-bottom: 16px;
  border-radius: 14px;
  color: #fff;
  background:
    radial-gradient(circle at 92% -50%, rgba(255, 255, 255, 0.16), transparent 44%),
    radial-gradient(circle at -6% 120%, rgba(62, 207, 142, 0.14), transparent 40%),
    radial-gradient(rgba(255, 255, 255, 0.07) 1px, transparent 1px),
    var(--ky-side-grad);
  background-size: 100% 100%, 100% 100%, 18px 18px, 100% 100%;
  box-shadow: 0 12px 28px rgba(13, 43, 86, 0.18);
}
.hello-main {
  display: flex;
  align-items: baseline;
  gap: 12px;
}
.hello-title {
  font-size: 18px;
  font-weight: 700;
  letter-spacing: 0.4px;
}
.hello-date {
  font-size: 12.5px;
  color: rgba(255, 255, 255, 0.6);
}
.hello-sub {
  margin-top: 4px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.68);
}
.hello-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: 999px;
  border: 1px solid rgba(255, 255, 255, 0.35);
  background: rgba(255, 255, 255, 0.13);
  backdrop-filter: blur(6px);
  font-size: 12.5px;
  white-space: nowrap;
}

/* 统计卡（可点击） */
.stat-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}
.stat-card {
  position: relative;
  overflow: hidden;
  padding: 18px 20px;
  display: flex;
  align-items: center;
  gap: 14px;
  cursor: pointer;
  transition: transform 0.25s var(--ease-out), box-shadow 0.25s;
}
.stat-card:hover {
  transform: translateY(-3px);
  box-shadow: 0 12px 26px rgba(15, 38, 74, 0.1);
}
.stat-card:hover .stat-arrow {
  opacity: 1;
  transform: translateX(0);
}
.stat-card::after {
  content: '';
  position: absolute;
  top: -34px;
  right: -34px;
  width: 90px;
  height: 90px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(30, 111, 224, 0.09), transparent 70%);
}
.stat-icon {
  width: 46px;
  height: 46px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 13px;
  transition: all 0.25s var(--ease-out);
}
.stat-card.blue .stat-icon {
  background: #e9f1fd;
  color: var(--ky-primary);
}
.stat-card.green .stat-icon {
  background: #e8f8f1;
  color: #2fa772;
}
.stat-card.orange .stat-icon {
  background: #fdf3e3;
  color: #cf8a1f;
}
.stat-card.cyan .stat-icon {
  background: #e6f5fa;
  color: #2196b8;
}
.stat-card:hover .stat-icon {
  transform: scale(1.06);
}
.stat-card.blue:hover .stat-icon { background: var(--ky-primary); color: #fff }
.stat-card.green:hover .stat-icon { background: #2fa772; color: #fff }
.stat-card.orange:hover .stat-icon { background: #e08c2f; color: #fff }
.stat-card.cyan:hover .stat-icon { background: #2196b8; color: #fff }
.stat-num {
  font-size: 25px;
  font-weight: 700;
  line-height: 1.15;
  font-variant-numeric: tabular-nums;
}
.stat-label {
  font-size: 12px;
  color: var(--ky-text-faint);
}
.stat-sub {
  margin-top: 2px;
  font-size: 11px;
  color: #b3bccb;
}
.stat-arrow {
  position: absolute;
  right: 14px;
  top: 50%;
  transform: translateY(-50%) translateX(-6px);
  color: var(--ky-primary);
  opacity: 0;
  transition: all 0.25s var(--ease-out);
}

/* 中排 */
.dash-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  margin-bottom: 16px;
  align-items: stretch;
}
.dist-card,
.sec-card {
  display: flex;
  flex-direction: column;
}
.dist-card .block-title,
.sec-card .block-title {
  flex-shrink: 0;
}
.dist-card > div:last-child {
  flex: 1;
  display: flex;
  align-items: center;
}
.block-title {
  position: relative;
  font-size: 14px;
  font-weight: 700;
  padding-left: 11px;
  margin-bottom: 16px;
}
.block-title::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3.5px;
  height: 14px;
  border-radius: 2px;
  background: var(--ky-primary);
}
.block-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.block-title-row .block-title {
  margin-bottom: 16px;
}
.block-sub {
  font-size: 12px;
  color: var(--ky-text-faint);
  margin-bottom: 16px;
}

/* ECharts 环形图 */
.chart-wrap {
  flex: 1;
  display: flex;
  align-items: center;
  min-height: 0;
}
.donut-hold {
  position: relative;
  flex: 1;
  min-width: 0;
}
.donut-chart {
  width: 100%;
  height: 200px;
}
/* 环心文字：CSS 精确锚定在环的数学圆心（32%, 50%） */
.donut-center {
  position: absolute;
  left: 32%;
  top: 50%;
  transform: translate(-50%, -50%);
  text-align: center;
  pointer-events: none;
}
.dc-num {
  font-size: 25px;
  font-weight: 700;
  line-height: 1.1;
  color: #1f2430;
  font-variant-numeric: tabular-nums;
}
.dc-label {
  margin-top: 2px;
  font-size: 11.5px;
  color: #98a3b3;
}

/* 安全中心 */

/* 最近使用轻列表 */
.recent-card {
  overflow: hidden;
}
.recent-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  padding: 16px 20px 6px;
}
.recent-head .block-title {
  margin-bottom: 0;
}
.recent-head .block-sub {
  margin-bottom: 0;
}
.recent-count {
  font-size: 12px;
  color: var(--ky-text-faint);
}
.recent-list {
  padding: 0 12px;
}
.recent-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 10px;
  border-radius: 10px;
  transition: background 0.18s ease;
}
.recent-row + .recent-row {
  border-top: 1px solid #f0f3f9;
}
.recent-row:hover {
  background: #f0f5fd;
}
.recent-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.recent-line {
  display: flex;
  align-items: center;
  gap: 8px;
}
.recent-name {
  font-weight: 600;
  font-size: 13.5px;
}
.recent-addr {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  color: #6b7a90;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.recent-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--ky-text-faint);
}
.meta-proto {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.5px;
  color: #47536b;
}
.meta-dot {
  color: #c3cbd9;
}
.recent-time {
  color: var(--ky-text-faint);
  font-size: 12px;
  white-space: nowrap;
}
.recent-foot {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 11px 0;
  margin-top: 2px;
  border-top: 1px solid var(--ky-border);
  font-size: 12.5px;
  color: var(--ky-primary);
  cursor: pointer;
  transition: background 0.18s ease;
}
.recent-foot:hover {
  background: #f5f9fe;
}
.recent-foot .el-icon {
  transition: transform 0.18s ease;
}
.recent-foot:hover .el-icon {
  transform: translateX(3px);
}

/* 空状态 */
.ky-empty.small {
  padding: 28px 16px;
}
.ky-empty.small .empty-icon {
  width: 52px;
  height: 52px;
  margin-bottom: 10px;
}

@media (max-width: 980px) {
  .stat-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .dash-row {
    grid-template-columns: 1fr;
  }
}
</style>
