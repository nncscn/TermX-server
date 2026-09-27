import { createRouter, createWebHistory } from 'vue-router'
import { getToken } from '@/api/client'
import { getSetupStatus } from '@/api/setup'
import AppLayout from '@/layouts/AppLayout.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue')
    },
    {
      // SSH 密钥独立页已下线，旧地址回首页
      path: '/keys',
      redirect: '/dashboard'
    },
    {
      // 找回密码已合并为登录卡片的背面，直接访问时跳到登录页并翻开背面
      path: '/forgot',
      redirect: (to) => ({
        path: '/login',
        query: { flip: '1', account: to.query.account }
      })
    },
    {
      path: '/setup',
      name: 'setup',
      component: () => import('@/views/SetupView.vue')
    },
    {
      path: '/',
      component: AppLayout,
      redirect: '/dashboard',
      children: [
        {
          path: 'dashboard',
          name: 'dashboard',
          component: () => import('@/views/DashboardView.vue'),
          meta: { title: '仪表盘' }
        },
        {
          path: 'credentials',
          name: 'credentials',
          component: () => import('@/views/CredentialsView.vue'),
          meta: { title: '连接凭据' }
        },
        {
          path: 'trash',
          name: 'trash',
          component: () => import('@/views/TrashView.vue'),
          meta: { title: '回收站' }
        },
        {
          path: 'settings',
          name: 'settings',
          component: () => import('@/views/SettingsView.vue'),
          meta: { title: '设置' }
        }
      ]
    }
  ]
})

// 未初始化（无账户）→ 强制进引导页；已初始化 → 禁入引导页。
// 状态未知（后端不可达）时不强制跳引导，维持常规登录流程，
let setupConfigured = null

async function loadSetupStatus() {
  try {
    const st = await getSetupStatus()
    setupConfigured = !!st.configured
  } catch {
    setupConfigured = null
  }
  return setupConfigured
}

/** 引导完成后由向导页回写，避免跳转时再等一次网络请求 */
export function setSetupConfigured(v) {
  setupConfigured = !!v
}

// 守卫：未初始化强制进引导（登录页也拦）；已初始化禁入引导页；
// 功能页需持有后端令牌
router.beforeEach(async (to) => {
  if (setupConfigured === null) await loadSetupStatus()
  if (setupConfigured === false && to.path !== '/setup') return '/setup'
  if (setupConfigured === true && to.path === '/setup') return '/login'
  if (to.path === '/login' || to.path === '/setup') return true
  if (!getToken()) return '/login'
  return true
})

export default router
