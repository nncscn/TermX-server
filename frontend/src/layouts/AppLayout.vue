<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import logoUrl from '@/assets/logo.png'
import { getRememberedName } from '@/api/local'
import { apiLogout } from '@/api/backend'
import { clearToken } from '@/api/client'
import { apiGetSettings } from '@/api/settings'
import { applyTrashRetention } from '@/utils/retention'

const route = useRoute()
const router = useRouter()

const menus = [
  { path: '/dashboard', title: '仪表盘', icon: 'Odometer' },
  { path: '/credentials', title: '连接凭据', icon: 'Connection' },
  { path: '/trash', title: '回收站', icon: 'Delete' },
  { path: '/settings', title: '设置', icon: 'Setting' }
]

// 各页面描述（顶栏副标题）
const descs = {
  '/dashboard': '总览密钥库状态与最近使用',
  '/credentials': '管理服务器地址、账号与密码',
  '/trash': '已删除的密钥与凭据，可恢复或彻底删除',
  '/settings': '主密码、自动锁定与数据备份'
}
const routeDesc = computed(() => descs[route.path] || '')

// 移动端抽屉菜单
const drawerOpen = ref(false)
watch(
  () => route.path,
  () => {
    drawerOpen.value = false
  }
)

function goMenu(path) {
  router.push(path)
  drawerOpen.value = false
}

const accountName = computed(() => getRememberedName() || 'admin')
const account = computed(() => ({ name: accountName.value, color: 'blue' }))

// 账户身份点配色（不设头像，用色点标识账户）
const ACCENT = { blue: '#1e6fe0', green: '#2fa772', orange: '#e08c2f', slate: '#8a97ab' }
const accentColor = computed(() => ACCENT[account.value?.color] || ACCENT.blue)

// 启动时同步账户偏好到本地缓存（数据库为事实源），再按保留策略清理回收站过期条目
onMounted(async () => {
  try {
    await apiGetSettings()
  } catch {
    /* 取不到设置时沿用本地缓存 */
  }
  applyTrashRetention()
})

let idleTimer = null
const IDLE_EVENTS = ['mousemove', 'mousedown', 'keydown', 'wheel', 'touchstart']

function scheduleIdleLock() {
  clearTimeout(idleTimer)
  const minutes = parseInt(localStorage.getItem('ky_autolock') || '0', 10)
  if (minutes > 0) idleTimer = setTimeout(autoLockNow, minutes * 60 * 1000)
}

async function autoLockNow() {
  try {
    await apiLogout()
  } catch {
    /* 会话可能已过期，继续本地清理 */
  }
  clearToken()
  // 整页刷新回登录页（而非组件内跳转）：彻底重置页面内存状态——
  // 图表实例、弹窗/过渡、各页面临时数据在整页加载后必然干净
  window.location.href = '/login?locked=1'
}

onMounted(() => {
  IDLE_EVENTS.forEach((ev) => window.addEventListener(ev, scheduleIdleLock, { passive: true }))
  scheduleIdleLock()
})
onUnmounted(() => {
  IDLE_EVENTS.forEach((ev) => window.removeEventListener(ev, scheduleIdleLock))
  clearTimeout(idleTimer)
})

async function onLogout() {
  await apiLogout()
  clearToken()
  ElMessage.success('已退出登录')
  router.push('/login')
}
</script>

<template>
  <div class="layout">
    <!-- 侧边栏（桌面） -->
    <aside class="side">
      <div class="side-brand" @click="router.push('/dashboard')">
        <img :src="logoUrl" class="side-logo" alt="TermX" />
        <div>
          <div class="side-name">TermX</div>
          <div class="side-sub">本地加密 · 开源版</div>
        </div>
      </div>

      <el-menu router :default-active="route.path" class="side-menu">
        <el-menu-item v-for="m in menus" :key="m.path" :index="m.path">
          <el-icon><component :is="m.icon" /></el-icon>
          <span>{{ m.title }}</span>
        </el-menu-item>
      </el-menu>

      <div class="side-foot">
        <div class="foot-status">
          <span class="dot"></span>
          密钥库已解锁
        </div>
        <div class="foot-ver">v0.1.0 · 数据仅保存在本机</div>
      </div>
    </aside>

    <!-- 移动端抽屉菜单 -->
    <el-drawer v-model="drawerOpen" direction="ltr" size="248px" :with-header="false" class="nav-drawer">
      <div class="drawer-body">
        <div class="side-brand" @click="goMenu('/dashboard')">
          <img :src="logoUrl" class="side-logo" alt="TermX" />
          <div>
            <div class="side-name">TermX</div>
            <div class="side-sub">本地加密 · 开源版</div>
          </div>
        </div>

        <div class="drawer-menu">
          <div
            v-for="m in menus"
            :key="m.path"
            class="drawer-item"
            :class="{ active: route.path === m.path }"
            @click="goMenu(m.path)"
          >
            <el-icon><component :is="m.icon" /></el-icon>
            <span>{{ m.title }}</span>
          </div>
        </div>

        <div class="drawer-foot">
          <div class="foot-status">
            <span class="dot"></span>
            密钥库已解锁
          </div>
          <div class="foot-ver">v0.1.0 · 数据仅保存在本机</div>
        </div>
      </div>
    </el-drawer>

    <!-- 主区域 -->
    <div class="body">
      <header class="topbar">
        <div class="topbar-left">
          <button class="menu-btn" type="button" aria-label="打开菜单" @click="drawerOpen = true">
            <el-icon :size="20"><Menu /></el-icon>
          </button>
          <div class="topbar-text">
            <div class="topbar-title">{{ route.meta?.title || '' }}</div>
            <div v-if="routeDesc" class="topbar-desc">{{ routeDesc }}</div>
          </div>
        </div>
        <div class="topbar-right">
          <div v-if="account" class="account-chip" :title="`账户：${account.name}`">
            <span class="acc-dot" :style="{ background: accentColor }"></span>
            <span class="name">{{ account.name }}</span>
          </div>
          <span class="topbar-divider"></span>
          <div class="status-chip" title="数据仅保存在本机">
            <span class="dot"></span>
            本机模式
          </div>
          <button type="button" class="logout-btn" @click="onLogout">
            <el-icon :size="14"><SwitchButton /></el-icon>
            退出登录
          </button>
        </div>
      </header>

      <main class="content">
        <router-view v-slot="{ Component }">
          <transition name="page-fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </main>
    </div>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  height: 100vh;
  overflow: hidden;
}

/* 侧边栏（桌面） */
.side {
  width: 216px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
  border-right: 1px solid var(--ky-border);
}

.side-brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 18px 18px 16px;
  cursor: pointer;
}
.side-logo {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  box-shadow: 0 8px 18px rgba(23, 58, 105, 0.25);
}
.side-name {
  font-size: 15.5px;
  font-weight: 700;
  letter-spacing: 0.5px;
  line-height: 1.2;
}
.side-sub {
  margin-top: 3px;
  font-size: 10.5px;
  color: var(--ky-text-faint);
  letter-spacing: 0.3px;
}

.side-menu {
  border-right: none;
  padding: 6px 10px;
  flex: 1;
}
.side-menu :deep(.el-menu-item) {
  border-radius: 10px;
  height: 44px;
  line-height: 44px;
  margin-bottom: 2px;
  color: var(--ky-text-sub);
  transition: all 0.22s var(--ease-out);
}
.side-menu :deep(.el-menu-item:hover) {
  background: #f2f6fc;
  color: var(--ky-primary);
}
.side-menu :deep(.el-menu-item.is-active) {
  background: var(--ky-primary-soft);
  color: var(--ky-primary);
  font-weight: 600;
}

.side-foot {
  padding: 14px 18px 16px;
  border-top: 1px solid var(--ky-border);
}
.foot-status {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  color: var(--ky-text-sub);
}
.foot-status .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--ky-accent);
  box-shadow: 0 0 0 3px rgba(62, 207, 142, 0.2);
}
.foot-ver {
  margin-top: 6px;
  font-size: 10.5px;
  color: var(--ky-text-faint);
}

/* 汉堡按钮（仅移动端显示） */
.menu-btn {
  display: none;
  width: 38px;
  height: 38px;
  margin-right: 4px;
  border: none;
  border-radius: 10px;
  background: transparent;
  color: var(--ky-text-main);
  cursor: pointer;
  align-items: center;
  justify-content: center;
  transition: background 0.2s;
}
.menu-btn:hover {
  background: #f2f6fc;
}

/* 抽屉菜单内部 */
.drawer-body {
  display: flex;
  flex-direction: column;
  height: 100%;
  margin: -16px -12px;
}
.drawer-menu {
  flex: 1;
  padding: 8px 12px;
}
.drawer-item {
  display: flex;
  align-items: center;
  gap: 12px;
  height: 46px;
  padding: 0 14px;
  border-radius: 10px;
  margin-bottom: 2px;
  color: var(--ky-text-sub);
  font-size: 14.5px;
  cursor: pointer;
  transition: all 0.22s var(--ease-out);
}
.drawer-item .el-icon {
  font-size: 17px;
}
.drawer-item:hover {
  background: #f2f6fc;
  color: var(--ky-primary);
}
.drawer-item.active {
  background: var(--ky-primary-soft);
  color: var(--ky-primary);
  font-weight: 600;
}
.drawer-foot {
  padding: 14px 20px 18px;
  border-top: 1px solid var(--ky-border);
}

/* 主区域 */
.body {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.topbar {
  min-height: 56px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 16px;
  background: #fff;
  border-bottom: 1px solid var(--ky-border);
  gap: 8px;
}
.topbar-left {
  display: flex;
  align-items: center;
  min-width: 0;
}
.topbar-desc {
  font-size: 12px;
  color: var(--ky-text-faint);
  margin-top: 1px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.topbar-title {
  font-size: 16px;
  font-weight: 700;
  line-height: 1.3;
  white-space: nowrap;
}
.topbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}
.account-chip {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 5px 13px;
  border-radius: 999px;
  background: #f5f8fc;
  border: 1px solid #e4ebf5;
}
.account-chip .acc-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}
.account-chip .name {
  font-size: 12.5px;
  font-weight: 600;
  color: #3d4a63;
  white-space: nowrap;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.topbar-divider {
  width: 1px;
  height: 16px;
  background: #e0e6f0;
  margin: 0 2px;
}
.logout-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 14px;
  border-radius: 999px;
  border: 1px solid #d4e0f1;
  background: transparent;
  color: var(--ky-primary);
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.18s ease;
}
.logout-btn:hover {
  background: var(--ky-primary-soft);
  border-color: #b9d3f0;
}
.status-chip {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  border-radius: 999px;
  background: #eef9f4;
  border: 1px solid #d3f0e4;
  color: #2e8f6d;
  font-size: 12px;
}
.status-chip .dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ky-accent);
}

.content {
  flex: 1;
  overflow: auto;
  padding: 24px;
  background:
    radial-gradient(circle at 12% -8%, rgba(30, 111, 224, 0.06), transparent 42%),
    radial-gradient(circle at 108% 108%, rgba(62, 207, 142, 0.055), transparent 42%),
    radial-gradient(#d9dfec 1px, transparent 1px),
    var(--ky-bg);
  background-size:
    100% 100%,
    100% 100%,
    22px 22px,
    100% 100%;
}

/* ===== 移动端（≤880px）：侧边栏换抽屉 ===== */
@media (max-width: 880px) {
  .side {
    display: none;
  }
  .menu-btn {
    display: flex;
  }
  .status-chip,
  .topbar-divider {
    display: none;
  }
  .account-chip .name {
    display: none;
  }
  .account-chip {
    padding: 5px 7px;
  }
  .topbar-desc {
    display: none;
  }
  .topbar {
    padding: 6px 10px;
  }
  .content {
    padding: 12px 12px 20px;
  }
}
</style>
