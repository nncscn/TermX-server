<script setup>
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import logoUrl from '@/assets/logo.png'
import { testConnection, testServer, completeSetup, getSetupStatus } from '@/api/setup'
import { setSetupConfigured } from '@/router'
import { copyText } from '@/utils/format'

const route = useRoute()
const router = useRouter()

//（路由守卫之外的第二道防线，防守卫状态未就绪的窗口期）
onMounted(async () => {
  try {
    const st = await getSetupStatus()
    if (st && st.configured) {
      setSetupConfigured(true)
      router.replace('/login')
    }
  } catch {
    /* 后端不可达时交给路由守卫的常规流程 */
  }
})

// 阶段：0 欢迎 → 1 数据库 → 2 服务设置 → 3 安全设置 → 4 确认 → 5 完成
const step = ref(0)

// ---------- 许可协议 ----------
const licenseAgreed = ref(false)
const showLicense = ref(false)

// ---------- 数据库配置 ----------
// 存储模式：sqlite（本地自建库）| external（外部数据库）
const dbType = ref('sqlite')

const ENGINES = {
  mysql: { label: 'MySQL', port: 3306, icon: 'Connection' },
  postgresql: { label: 'PostgreSQL', port: 5432, icon: 'Coin' },
  sqlserver: { label: 'SQL Server', port: 1433, icon: 'Box' }
}
const dbEngine = ref('mysql')

const connForm = reactive({
  host: '127.0.0.1',
  port: 3306,
  username: '',
  password: '',
  database: 'ky'
})

function pickType(type) {
  dbType.value = type
  testResult.value = null
}

// 切换数据库类型时重置为该类型的默认端口
function pickEngine(key) {
  dbEngine.value = key
  connForm.port = ENGINES[key].port
  testResult.value = null
}

const testing = ref(false)
// null 未测试 | { ok, message }
const testResult = ref(null)

const connFormValid = computed(
  () => connForm.host && connForm.port && connForm.username && connForm.database
)

const dbStepOk = computed(
  () => dbType.value === 'sqlite' || connFormValid.value
)

/** 下一步（数据库步）：自动执行连接测试，通过才前进 */
const dbStepLoading = ref(false)
async function dbStepNext() {
  if (dbType.value === 'sqlite') {
    step.value = 2
    return
  }
  dbStepLoading.value = true
  await onTestConnection()
  dbStepLoading.value = false
  if (testResult.value?.ok) step.value = 2
  else ElMessage.error(testResult.value?.message || '连接失败')
}

async function onTestConnection() {
  testing.value = true
  testResult.value = null
  try {
    const d = await testConnection({ engine: dbEngine.value, ...connForm })
    testResult.value = {
      ok: !!d.ok,
      warn: d.ok && d.can_create_db === false,
      message: d.privilege_hint ? `${d.message}：${d.privilege_hint}` : d.message
    }
  } catch (e) {
    testResult.value = { ok: false, message: e.message }
  } finally {
    testing.value = false
  }
}

// ---------- 服务配置 ----------
const svcForm = reactive({
  host: '127.0.0.1',
  port: 18080,
  https: false,
  certPath: 'certs/server.crt',
  keyPath: 'certs/server.key'
})

// 默认 /api/client：后端 TermX 同步组的真实挂载前缀（客户端在此基地址后自拼 /ext/verify 等子路径）
const syncForm = reactive({ enabled: true, path: '/api' })

/** 接口路径合法性：以 / 开头，段内仅字母/数字/-/_，允许多级（默认 /api/client） */
const syncPathValid = computed(() => /^\/[A-Za-z0-9_/-]{1,64}$/.test(syncForm.path) && !syncForm.path.endsWith('//'))

/** 输入即规范化：滤掉非法字符、保证以 / 开头、合并连续斜杠、去掉末尾斜杠 */
function onSyncPathInput(val) {
  let p = String(val).replace(/[^A-Za-z0-9/_-]/g, '')
  if (!p.startsWith('/')) p = '/' + p
  p = p.replace(/\/+/g, '/')
  if (p.length > 1 && p.endsWith('/')) p = p.slice(0, -1)
  syncForm.path = p
}

/** 同步接口完整地址（监听 0.0.0.0 时按本机回环展示，与访问地址预览一致） */
const syncUrl = computed(() => {
  const host = svcForm.host.trim() === '0.0.0.0' ? '127.0.0.1' : svcForm.host.trim()
  return `${svcForm.https ? 'https' : 'http'}://${host}:${svcForm.port}${
    syncPathValid.value ? syncForm.path : '/api'
  }`
})

/** 复制完整地址，粘贴到 TermX 桌面客户端的服务器设置 */
async function copySyncUrl() {
  const ok = await copyText(syncUrl.value)
  ok ? ElMessage.success('接口地址已复制，请粘贴到 TermX 客户端') : ElMessage.error('复制失败')
}

const srvTesting = ref(false)
const srvTestResult = ref(null) // null 未验证 | { ok, message }

async function onTestServer() {
  srvTesting.value = true
  srvTestResult.value = null
  try {
    srvTestResult.value = await testServer({ ...svcForm })
  } catch (e) {
    srvTestResult.value = { ok: false, message: e.message }
  } finally {
    srvTesting.value = false
  }
}

// 服务配置任何变动后验证结果失效，需重新验证
watch(
  () => [svcForm.host, svcForm.port, svcForm.https, svcForm.certPath, svcForm.keyPath],
  () => {
    srvTestResult.value = null
  }
)

const remoteLoopbackRisk = computed(() => {
  const browserHost = location.hostname || ''
  const isRemote = !!browserHost && !/^(127\.|localhost$|\[::1\]|::1$)/.test(browserHost)
  const isLoopback = /^(127\.|localhost$|::1$)/.test(svcForm.host.trim())
  return isRemote && isLoopback
})

// 访问地址实时预览；0.0.0.0 表示监听所有网卡，预览按本机回环展示
const siteUrl = computed(() => {
  const host = svcForm.host.trim() === '0.0.0.0' ? '127.0.0.1' : svcForm.host.trim()
  return `${svcForm.https ? 'https' : 'http'}://${host}:${svcForm.port}`
})

const svcStepOk = computed(
  () =>
    !!svcForm.host.trim() &&
    svcForm.port >= 1 &&
    svcForm.port <= 65535 &&
    (!svcForm.https || (svcForm.certPath && svcForm.keyPath)) &&
    (!syncForm.enabled || syncPathValid.value)
)

/** 下一步（服务设置步）：自动执行配置验证，通过才前进 */
const svcStepLoading = ref(false)
async function svcStepNext() {
  svcStepLoading.value = true
  await onTestServer()
  svcStepLoading.value = false
  if (srvTestResult.value?.ok) step.value = 3
  else ElMessage.error(srvTestResult.value?.message || '配置验证失败')
}

// ---------- 安全设置（账户 + 主密码） ----------
const secForm = reactive({ account: '', password: '', confirm: '' })

const pwdMismatch = computed(
  () => secForm.confirm.length > 0 && secForm.password !== secForm.confirm
)

const strengthLevel = computed(() => {
  const p = secForm.password
  if (!p) return 0
  let score = 1
  if (p.length >= 8) score++
  if (p.length >= 12) score++
  if (/[a-z]/.test(p) && /[A-Z]/.test(p)) score++
  if (/\d/.test(p) && /[^a-zA-Z0-9]/.test(p)) score++
  return Math.min(score, 4)
})

const strengthText = computed(() =>
  ['未设置', '弱', '一般', '较强', '强'][strengthLevel.value]
)

const secStepOk = computed(
  () =>
    !!secForm.account.trim() &&
    secForm.password.length >= 8 &&
    !pwdMismatch.value
)

// ---------- 初始化与完成监控 ----------
const initializing = ref(false)
const setupResult = ref(null) // { recovery_key, access_url, phases }
const restartReady = ref(false) // 轮询到服务重启完成且已初始化
const restartTimeout = ref(false) // 30 秒未就绪
let sqliteFileOverride = '' // 同名库冲突弹窗确认后使用的建议文件名

const PHASE_LABELS = ['数据库已就绪', '数据表已创建', '账户已创建', '配置已写入', '服务重启中']
const phasesDone = computed(() => {
  if (!setupResult.value) return 0
  const n = setupResult.value.phases?.length || 0
  return restartReady.value ? n : n - 1
})

function buildPayload() {
  return {
    database:
      dbType.value === 'external'
        ? {
            engine: dbEngine.value,
            host: connForm.host,
            port: connForm.port,
            user: connForm.username,
            password: connForm.password,
            name: connForm.database
          }
        : { engine: 'sqlite', file: sqliteFileOverride },
    server: {
      host: svcForm.host,
      port: svcForm.port,
      https: svcForm.https,
      cert_file: svcForm.certPath,
      key_file: svcForm.keyPath
    },
    termx_base_path: syncForm.enabled && syncPathValid.value ? syncForm.path : '',
    username: secForm.account.trim(),
    password: secForm.password,
  }
}

async function onFinish() {
  if (initializing.value) return
  initializing.value = true
  try {
    const res = await completeSetup(buildPayload())
    await enterMonitor(res)
  } catch (e) {
    if (e.code === 40901 && e.data?.suggested_name) {
      await onDbNameConflict(e.data)
    } else {
      ElMessage.error(e.message || '初始化失败，请检查后重试')
    }
  } finally {
    initializing.value = false
  }
}

async function onDbNameConflict({ requested_name, suggested_name }) {
  const isSqlite = dbType.value === 'sqlite'
  try {
    await ElMessageBox.confirm(
      `检测到「${requested_name}」已存在（已有数据一律不触碰）。${
        isSqlite ? '确认后自动改用' : '已为你填入'
      }建议名称「${suggested_name}」继续初始化。`,
      '目标库已存在',
      { confirmButtonText: '使用建议名称', cancelButtonText: '返回修改', type: 'warning' }
    )
    if (isSqlite) {
      sqliteFileOverride = suggested_name
      const res = await completeSetup(buildPayload())
      await enterMonitor(res)
    } else {
      connForm.database = suggested_name
      ElMessage.success(`数据库名已改为「${suggested_name}」，请再次点击"完成初始化"`)
    }
  } catch {
    /* 用户取消：留在确认页自行修改 */
  }
}

/**
 * 引导可能改变了监听地址/端口，避免完成后登录失联：
 * - 同源部署（单文件）且端口未变：保持同源请求，不写死地址（0.0.0.0 的展示
 *   地址是 127.0.0.1，远端浏览器不可达，写死会导致轮询/登录打到错误机器）；
 * - 端口变化或跨源开发模式：切换服务地址（同源部署保留当前主机名只换端口）。
 */
function applyNewApiBase(accessUrl) {
  try {
    const u = new URL(accessUrl)
    if (!localStorage.getItem('ky_api_base') && u.port === location.port) {
      return
    }
    const host = localStorage.getItem('ky_api_base')
      ? u.host
      : `${location.hostname}:${u.port || (u.protocol === 'https:' ? 443 : 80)}`
    localStorage.setItem('ky_api_base', host)
    localStorage.setItem('ky_api_scheme', u.protocol.replace(':', ''))
  } catch {
    /* 地址解析失败时保留当前值 */
  }
}

/** 轮询等待服务重启完成（容忍重启窗口期的连接失败，30 秒超时） */
async function waitRestartReady() {
  const deadline = Date.now() + 30000
  while (Date.now() < deadline) {
    try {
      const st = await getSetupStatus()
      if (st && st.configured) return true
    } catch {
      /* 重启窗口期请求失败属预期，继续轮询 */
    }
    await new Promise((r) => setTimeout(r, 1000))
  }
  return false
}

async function enterMonitor(res) {
  setupResult.value = res
  restartReady.value = false
  restartTimeout.value = false
  setSetupConfigured(true)
  applyNewApiBase(res.access_url)
  step.value = 6
  restartReady.value = await waitRestartReady()
  if (!restartReady.value) restartTimeout.value = true
}

async function onCopyTermxKey() {
  const ok = await copyText(setupResult.value?.termx_key || '')
  ok ? ElMessage.success('TermX 接入密钥已复制') : ElMessage.error('复制失败')
}

async function onCopyRecovery() {
  const ok = await copyText(setupResult.value?.recovery_key || '')
  ok ? ElMessage.success('恢复密钥已复制') : ElMessage.error('复制失败')
}

const goLoginBusy = ref(false)
const goLoginError = ref('')

async function goLogin() {
  if (goLoginBusy.value || !setupResult.value) return
  goLoginBusy.value = true
  goLoginError.value = ''
  const account = encodeURIComponent(secForm.account.trim())

  let base = location.origin
  try {
    const nu = new URL(setupResult.value.access_url)
    let host = nu.hostname
    if ((host === '127.0.0.1' || host === '::1') &&
      !/^(127\.|localhost$|\[::1\]$)/.test(location.hostname)) {
      host = location.hostname
    }
    base = `${nu.protocol}//${host}:${nu.port || (nu.protocol === 'https:' ? 443 : 80)}`
  } catch {
    /* 地址解析失败用当前源 */
  }

  // 可达性探测（2 秒）：不可达给出明确诊断而不是无反应的跳转
  try {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), 2000)
    const resp = await fetch(`${base}/api/v1/health`, { signal: ctrl.signal })
    clearTimeout(timer)
    if (!resp.ok) throw new Error('bad status')
    window.location.replace(`${base}/login?account=${account}`)
    return
  } catch {
    goLoginError.value = remoteLoopbackRisk.value
      ? '服务只监听了 127.0.0.1（仅服务器本机可访问），当前浏览器无法进入。请在服务器本机的浏览器打开，或把 data/config.yaml 的 server.host 改为 0.0.0.0 后重启服务'
      : `暂时无法连接 ${base}（服务可能仍在重启）。稍后再试，或手动访问 ${base}/login`
  } finally {
    goLoginBusy.value = false
  }
}

function downloadRecovery() {
  const blob = new Blob([setupResult.value?.recovery_key || ''], { type: 'text/plain' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = 'termx-recovery.key'
  a.click()
  URL.revokeObjectURL(url)
  ElMessage.success('恢复密钥文件已下载，请离线妥善保存')
}

// ---------- 侧栏步骤 ----------
const steps = [
  { title: '欢迎', desc: '了解TermX' },
  { title: '数据库', desc: '选择存储方式' },
  { title: '服务设置', desc: '地址与端口' },
  { title: '安全设置', desc: '设置主密码' },
  { title: '初始化', desc: '确认并完成' }
]

function stepState(i) {
  if (step.value >= 6 || i < step.value) return 'done'
  if (i === step.value) return 'active'
  return 'upcoming'
}

// ---------- 确认页信息 ----------
const engineInfo = computed(() => ENGINES[dbEngine.value])

const modeInfo = computed(() =>
  dbType.value === 'sqlite'
    ? {
        name: '本地模式',
        tag: '推荐',
        desc: 'SQLite 内嵌数据库，程序自动创建，零安装',
        icon: 'Monitor'
      }
    : {
        name: `${engineInfo.value.label} 数据库`,
        tag: '',
        desc: `连接外部 ${engineInfo.value.label} 服务器，多用户集中部署`,
        icon: engineInfo.value.icon
      }
)

const confirmItems = computed(() => {
  const items = []
  if (dbType.value === 'sqlite') {
    items.push({ label: '备份方式', value: '复制 data 目录即可完成' })
  } else {
    items.push({ label: '服务器', value: `${connForm.host}:${connForm.port}`, code: true })
    items.push({ label: '数据库类型', value: engineInfo.value.label })
    items.push({ label: '用户名', value: connForm.username })
    items.push({ label: '数据库名', value: connForm.database, code: true })
  }
  items.push({ label: '账户', value: secForm.account.trim() || '-' })
  items.push({ label: '数据加密', value: '主密钥由系统自动生成' })
  items.push({ label: '同步接口', value: syncForm.enabled ? syncUrl.value : '已关闭', code: true })
  items.push({ label: '访问地址', value: siteUrl.value, code: true })
  items.push({ label: '访问协议', value: svcForm.https ? 'HTTPS（加密传输）' : 'HTTP' })
  items.push({ label: '主密码', value: '已设置（登录并加密密钥库）' })
  return items
})
</script>

<template>
  <div class="setup-page">
    <!-- 背景装饰光斑 -->
    <div class="glow glow-a"></div>
    <div class="glow glow-b"></div>

    <div class="wizard">
      <!-- 左侧品牌栏 -->
      <aside class="side">
        <div class="side-deco"></div>

        <div class="side-brand">
          <img :src="logoUrl" class="brand-logo" alt="TermX" />
          <div class="brand-name">TermX</div>
          <div class="brand-sub">自托管凭据管理服务 · 安装向导</div>
        </div>

        <ul class="side-steps">
          <li v-for="(s, i) in steps" :key="s.title" :class="stepState(i)">
            <div class="step-dot">
              <el-icon v-if="stepState(i) === 'done'"><Check /></el-icon>
              <template v-else>{{ i + 1 }}</template>
            </div>
            <div class="step-info">
              <div class="step-title">{{ s.title }}</div>
              <div class="step-desc">{{ s.desc }}</div>
            </div>
          </li>
        </ul>

        <div class="side-foot">
          <div class="foot-line"><el-icon><Lock /></el-icon>数据仅保存在本机</div>
          <div class="foot-ver">TermX · 开源版 v0.1.0</div>
        </div>
      </aside>

      <!-- 右侧内容区 -->
      <section class="main">
        <div class="content">
          <transition name="step" mode="out-in">
            <!-- 第 1 步：欢迎 -->
            <div v-if="step === 0" key="welcome" class="pane">
              <div class="hero-icon"><el-icon :size="34"><Key /></el-icon></div>
              <h2>欢迎使用TermX</h2>
              <p class="pane-sub">
                一款本地优先的 SSH 凭据管理工具，密钥与密码只留在你的机器上。
              </p>
              <div class="feature-grid">
                <div class="feature">
                  <div class="feature-icon"><el-icon :size="20"><Key /></el-icon></div>
                  <div class="feature-title">SSH 密钥管理</div>
                  <div class="feature-desc">私钥加密存储，按主机组织</div>
                </div>
                <div class="feature">
                  <div class="feature-icon"><el-icon :size="20"><Lock /></el-icon></div>
                  <div class="feature-title">密码与连接方式</div>
                  <div class="feature-desc">连接参数统一保管、快速取用</div>
                </div>
                <div class="feature">
                  <div class="feature-icon"><el-icon :size="20"><FolderOpened /></el-icon></div>
                  <div class="feature-title">完全本地运行</div>
                  <div class="feature-desc">零安装数据库，数据不出本机</div>
                </div>
              </div>

              <!-- 开源许可 -->
              <div class="license-row">
                <el-checkbox v-model="licenseAgreed">我已阅读并同意</el-checkbox>
                <el-link type="primary" underline="never" @click="showLicense = true">
                  《TermX 开源许可证》
                </el-link>
              </div>
            </div>

            <!-- 第 2 步：选择数据库 -->
            <div v-else-if="step === 1" key="database" class="pane pane-top">
              <h2>选择数据存储方式</h2>
              <p class="pane-sub">两种模式随时可以切换，数据可迁移。</p>

              <div class="option-grid">
                <div
                  class="option-card"
                  :class="{ active: dbType === 'sqlite' }"
                  @click="pickType('sqlite')"
                >
                  <transition name="pop">
                    <el-icon v-if="dbType === 'sqlite'" class="option-check">
                      <CircleCheckFilled />
                    </el-icon>
                  </transition>
                  <div class="option-head">
                    <div class="icon-sq icon-sqlite"><el-icon :size="20"><Monitor /></el-icon></div>
                    <div>
                      <div class="option-title">
                        本地模式
                        <el-tag size="small" effect="light" round>推荐</el-tag>
                      </div>
                      <div class="option-desc">SQLite 内嵌数据库，零安装</div>
                    </div>
                  </div>
                  <ul class="option-points">
                    <li><el-icon><Check /></el-icon>程序自动建库建表，开箱即用</li>
                    <li><el-icon><Check /></el-icon>数据保存在本地 data 目录</li>
                    <li><el-icon><Check /></el-icon>适合个人与单机使用</li>
                  </ul>
                </div>

                <div
                  class="option-card"
                  :class="{ active: dbType === 'external' }"
                  @click="pickType('external')"
                >
                  <transition name="pop">
                    <el-icon v-if="dbType === 'external'" class="option-check">
                      <CircleCheckFilled />
                    </el-icon>
                  </transition>
                  <div class="option-head">
                    <div class="icon-sq icon-ext"><el-icon :size="20"><Connection /></el-icon></div>
                    <div>
                      <div class="option-title">外部数据库</div>
                      <div class="option-desc">连接已有数据库服务器</div>
                    </div>
                  </div>
                  <ul class="option-points">
                    <li><el-icon><Check /></el-icon>适合多用户集中部署</li>
                    <li><el-icon><Check /></el-icon>自动创建数据库与数据表</li>
                    <li><el-icon><Check /></el-icon>支持 MySQL / PostgreSQL / SQL Server</li>
                  </ul>
                </div>
              </div>

              <!-- 本地模式说明面板 -->
              <div class="expand" :class="{ open: dbType === 'sqlite' }">
                <div class="expand-inner">
                  <div class="info-panel">
                    <div class="info-title">
                      <el-icon><InfoFilled /></el-icon>本地模式说明
                    </div>
                    <div class="info-rows">
                      <div class="info-row">
                        <el-icon color="var(--ky-accent)"><CircleCheck /></el-icon>
                        <span>首次启动自动创建数据库文件并建表</span>
                      </div>
                      <div class="info-row">
                        <el-icon color="var(--ky-accent)"><CircleCheck /></el-icon>
                        <span>备份与迁移：复制数据目录到新机器即可</span>
                      </div>
                      <div class="info-row">
                        <el-icon color="var(--ky-accent)"><CircleCheck /></el-icon>
                        <span>之后可在 data/config.yaml 中切换为外部数据库</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <div class="expand" :class="{ open: dbType === 'external' }">
                <div class="expand-inner">
                  <div class="conn-panel">
                    <div class="engine-row">
                      <span class="engine-label">数据库类型</span>
                      <div class="engine-pills">
                        <button
                          v-for="(e, key) in ENGINES"
                          :key="key"
                          type="button"
                          class="engine-pill"
                          :class="{ on: dbEngine === key }"
                          @click="pickEngine(key)"
                        >
                          {{ e.label }}
                        </button>
                      </div>
                    </div>

                    <el-form label-position="top" class="conn-form" @submit.prevent>
                      <div class="form-row">
                        <el-form-item label="主机地址" required>
                          <el-input v-model="connForm.host" placeholder="127.0.0.1" />
                        </el-form-item>
                        <el-form-item label="端口" required>
                          <el-input-number
                            v-model="connForm.port"
                            :min="1"
                            :max="65535"
                            controls-position="right"
                            style="width: 100%"
                          />
                        </el-form-item>
                      </div>
                      <div class="form-row">
                        <el-form-item label="用户名" required>
                          <el-input v-model="connForm.username" placeholder="root" />
                        </el-form-item>
                        <el-form-item label="密码">
                          <el-input
                            v-model="connForm.password"
                            type="password"
                            show-password
                            placeholder="••••••"
                          />
                        </el-form-item>
                      </div>
                      <div class="form-row form-row-end">
                        <el-form-item label="数据库名" required>
                          <el-input v-model="connForm.database" placeholder="ky" />
                        </el-form-item>
                        <el-button
                          class="test-btn"
                          :loading="testing"
                          :disabled="!connFormValid"
                          @click="onTestConnection"
                        >
                          <el-icon style="margin-right: 6px"><Link /></el-icon>
                          测试连接
                        </el-button>
                      </div>
                    </el-form>

                    <div class="test-result-slot">
                      <el-alert
                        v-if="testResult"
                        :title="testResult.message"
                        :type="testResult.warn ? 'warning' : testResult.ok ? 'success' : 'error'"
                        :closable="false"
                        show-icon
                      />
                    </div>
                  </div>
                </div>
              </div>

              <div
                v-if="dbType === 'external'"
                class="test-hint"
                :style="{ visibility: dbStepOk ? 'hidden' : 'visible' }"
              >
                外部数据库将在点击下一步时自动验证连接
              </div>
            </div>

            <!-- 第 3 步：服务设置 -->
            <div v-else-if="step === 2" key="service" class="pane pane-top">
              <h2>服务设置</h2>
              <p class="pane-sub">设置程序的监听地址、端口与同步接口。</p>

              <div class="svc-panel">
                <el-form label-position="top" class="svc-form" @submit.prevent>
                  <div class="form-row">
                    <el-form-item label="监听地址" required>
                      <el-input v-model="svcForm.host" placeholder="127.0.0.1" />
                    </el-form-item>
                    <el-form-item label="端口" required>
                      <el-input-number
                        v-model="svcForm.port"
                        :min="1"
                        :max="65535"
                        controls-position="right"
                        style="width: 100%"
                      />
                    </el-form-item>
                  </div>

                  <!-- 访问协议 + 地址预览 -->
                  <div class="form-row url-row">
                    <el-form-item label="启用 HTTPS">
                      <el-switch v-model="svcForm.https" />
                    </el-form-item>
                    <el-form-item label="访问地址">
                      <div class="url-pill">
                        <el-icon><Link /></el-icon>
                        <span>{{ siteUrl }}</span>
                      </div>
                    </el-form-item>
                  </div>

                  <!-- HTTPS 证书设置 -->
                  <div class="expand" :class="{ open: svcForm.https }">
                    <div class="expand-inner">
                      <div class="form-row">
                        <el-form-item label="证书文件路径" required>
                          <el-input v-model="svcForm.certPath" placeholder="certs/server.crt" />
                        </el-form-item>
                        <el-form-item label="私钥文件路径" required>
                          <el-input v-model="svcForm.keyPath" placeholder="certs/server.key" />
                        </el-form-item>
                      </div>
                    </div>
                  </div>

                  <div class="sync-row">
                    <el-form-item label="同步接口">
                      <div class="sync-toggle">
                        <el-switch v-model="syncForm.enabled" />
                        <span class="sync-state" :class="{ on: syncForm.enabled }">
                          {{ syncForm.enabled ? '已启用' : '已关闭' }}
                        </span>
                        <span class="sync-hint">TermX 桌面客户端凭据同步</span>
                      </div>
                    </el-form-item>
                  </div>
                  <div class="expand" :class="{ open: syncForm.enabled }">
                    <div class="expand-inner">
                      <el-form-item label="接口前缀（不修改则使用默认值 /api）">
                        <div class="sync-url-row">
                          <el-input
                            :model-value="syncForm.path"
                            class="sync-path-input"
                            placeholder="/api"
                            @input="onSyncPathInput"
                          >
                            <template #prefix><el-icon><Link /></el-icon></template>
                          </el-input>
                          <el-button :disabled="!syncPathValid" @click="copySyncUrl">
                            <el-icon style="margin-right: 4px"><CopyDocument /></el-icon>复制地址
                          </el-button>
                        </div>
                        <div class="sync-url-pill">
                          <el-icon><Link /></el-icon>
                          <span>{{ syncUrl }}</span>
                        </div>
                        <div v-if="!syncPathValid" class="sync-url-tip">
                          路径需以 / 开头，段内仅限字母、数字、- _（留空即用默认 /api；TermX 客户端会自动在其后拼接 client/… 子路径）
                        </div>
                      </el-form-item>
                    </div>
                  </div>

                  <div class="form-row form-row-end srv-test-row">
                    <span class="srv-test-hint">
                      {{ srvTestResult?.ok ? '配置已通过验证' : '进入下一步前需先通过验证' }}
                    </span>
                    <el-button class="test-btn" :loading="srvTesting" @click="onTestServer">
                      <el-icon style="margin-right: 6px"><Link /></el-icon>验证配置
                    </el-button>
                  </div>
                  <div class="test-result-slot">
                    <el-alert
                      v-if="srvTestResult"
                      :title="srvTestResult.message"
                      :type="srvTestResult.ok ? 'success' : 'error'"
                      :closable="false"
                      show-icon
                    />
                  </div>
                  <div v-if="remoteLoopbackRisk" class="test-result-slot">
                    <el-alert
                      type="warning"
                      :closable="false"
                      show-icon
                      title="你正从其他设备远程访问：监听地址为 127.0.0.1 时，重启完成后只有服务器本机能打开页面，当前浏览器将无法访问。建议改为 0.0.0.0"
                    />
                  </div>
                </el-form>

                <div class="svc-caption">
                  <el-icon><InfoFilled /></el-icon>
                  <span>
                    监听 127.0.0.1 仅本机可访问；填 0.0.0.0
                    时局域网设备也可通过本机 IP 访问。外部数据库模式下数据存储在所连接的数据库中。
                  </span>
                </div>
              </div>
            </div>

            <div v-else-if="step === 3" key="security" class="pane pane-top">
              <h2>安全设置</h2>
              <p class="pane-sub">创建你的账户并设置主密码，用于登录与加密密钥库。</p>

              <div class="svc-panel">
                <el-form label-position="top" class="svc-form" @submit.prevent>
                  <el-form-item label="账户名" required>
                    <el-input
                      v-model="secForm.account"
                      placeholder="登录时使用的账户名，如 admin"
                      maxlength="12"
                      clearable
                    >
                        <template #prefix><el-icon><User /></el-icon></template>
                        </el-input>
                      </el-form-item>

                  <div class="form-row">
                    <el-form-item label="主密码" required>
                      <el-input
                        v-model="secForm.password"
                        type="password"
                        show-password
                        placeholder="至少 8 位字符"
                      />
                    </el-form-item>
                    <el-form-item label="确认主密码" required>
                      <el-input
                        v-model="secForm.confirm"
                        type="password"
                        show-password
                        placeholder="再次输入主密码"
                      />
                    </el-form-item>
                  </div>

                  <div class="strength">
                    <div class="strength-bars">
                      <span
                        v-for="i in 4"
                        :key="i"
                        :class="{ on: i <= strengthLevel }"
                        :data-lv="strengthLevel"
                      ></span>
                    </div>
                    <span class="strength-text" :data-lv="strengthLevel">
                      强度：{{ strengthText }}
                    </span>
                  </div>
                  <div
                    class="mismatch"
                    :style="{ visibility: pwdMismatch ? 'visible' : 'hidden' }"
                  >
                    <el-icon><CircleCloseFilled /></el-icon>
                    两次输入的密码不一致
                  </div>
                </el-form>
              </div>

              <!-- 三张迷你提示卡 -->
              <div class="sec-tiles">
                <div class="sec-tile">
                  <div class="tile-icon icon-blue"><el-icon :size="17"><Lock /></el-icon></div>
                  <div class="tile-title">双重作用</div>
                  <div class="tile-desc">登录身份验证，并加密密钥库全部数据</div>
                </div>
                <div class="sec-tile">
                  <div class="tile-icon icon-green"><el-icon :size="17"><InfoFilled /></el-icon></div>
                  <div class="tile-title">安全建议</div>
                  <div class="tile-desc">12 位以上，混合大小写、数字与符号，勿复用</div>
                </div>
                <div class="sec-tile tile-warn">
                  <div class="tile-icon icon-orange"><el-icon :size="17"><WarningFilled /></el-icon></div>
                  <div class="tile-title">不可恢复</div>
                  <div class="tile-desc">主密码遗忘后任何人（包括开发者）都无法找回数据</div>
                </div>
              </div>
            </div>

            <!-- 第 5 步：确认 -->
            <div v-else-if="step === 4" key="confirm" class="pane pane-top">
              <h2>确认初始化配置</h2>
              <p class="pane-sub">请核对以下配置，初始化约需数秒。</p>

              <div class="mode-banner">
                <div class="mode-icon">
                  <el-icon :size="26"><component :is="modeInfo.icon" /></el-icon>
                </div>
                <div class="mode-text">
                  <div class="mode-name">
                    {{ modeInfo.name }}
                    <el-tag v-if="modeInfo.tag" size="small" type="success" effect="light" round>
                      {{ modeInfo.tag }}
                    </el-tag>
                  </div>
                  <div class="mode-desc">{{ modeInfo.desc }}</div>
                </div>
              </div>

              <div class="confirm-grid">
                <div v-for="item in confirmItems" :key="item.label" class="confirm-item">
                  <div class="confirm-label">{{ item.label }}</div>
                  <div class="confirm-value">
                    <code v-if="item.code">{{ item.value }}</code>
                    <template v-else>{{ item.value }}</template>
                  </div>
                </div>
              </div>

              <div class="note">
                <el-icon><InfoFilled /></el-icon>
                <span>初始化将自动建库建表；之后可在 data/config.yaml 中调整</span>
              </div>
            </div>

            <!-- 第 6 步：初始化监控与完成 -->
            <div v-else key="done" class="pane pane-top">
              <h2>{{ restartReady ? '初始化完成' : '正在初始化' }}</h2>
              <p class="pane-sub">
                {{
                  restartReady
                    ? 'TermX已就绪，以下信息请妥善保存'
                    : restartTimeout
                      ? '等待服务重启超时，可手动确认状态'
                      : '正在执行初始化并重启服务，请勿关闭此页面'
                }}
              </p>

              <!-- 阶段清单 -->
              <div class="svc-panel phase-panel">
                <div
                  v-for="(p, i) in PHASE_LABELS"
                  :key="p"
                  class="phase-row"
                  :class="{ done: i < phasesDone, active: i === phasesDone && !restartReady }"
                >
                  <div class="phase-dot">
                    <el-icon v-if="i < phasesDone"><Check /></el-icon>
                    <el-icon v-else-if="i === phasesDone && !restartReady" class="spin">
                      <Loading />
                    </el-icon>
                    <template v-else>{{ i + 1 }}</template>
                  </div>
                  <span>{{ p }}</span>
                </div>
              </div>

              <!-- 超时提示 -->
              <div v-if="restartTimeout" class="warn-note">
                <el-icon><WarningFilled /></el-icon>
                <span>
                  等待服务重启超时。请手动访问 <code>{{ setupResult?.access_url }}</code>
                  确认是否就绪（启用 HTTPS 且为自签证书时，浏览器可能拦截自动检测）
                </span>
              </div>

              <div v-if="goLoginError" class="warn-note">
                <el-icon><WarningFilled /></el-icon>
                <span>{{ goLoginError }}</span>
              </div>

              <template v-if="restartReady">
                <div class="warn-note">
                  <el-icon><WarningFilled /></el-icon>
                  <span>
                    下方两个<b>密钥用途不同</b>，请分开保存：<b>恢复密钥</b>找回登录密码用；
                    <b>TermX 接入密钥</b>给 TermX 桌面客户端连接本服务器用（不用 TermX 可忽略）
                  </span>
                </div>
                <div class="recovery-box">
                  <div class="recovery-title">① 恢复密钥（忘记登录密码时自救用）</div>
                  <div class="recovery-key">{{ setupResult?.recovery_key }}</div>
                  <div class="recovery-hint">
                    使用时连同横杠完整复制粘贴即可（共 8 组；多余的空格或横杠会被自动忽略）。
                    它与主密钥是两样东西，请分开保存
                  </div>
                  <div class="recovery-actions">
                    <el-button size="small" @click="onCopyRecovery">
                      <el-icon style="margin-right: 4px"><CopyDocument /></el-icon>复制
                    </el-button>
                    <el-button size="small" @click="downloadRecovery">
                      <el-icon style="margin-right: 4px"><Download /></el-icon>下载
                    </el-button>
                  </div>
                </div>
                <div v-if="setupResult?.termx_key" class="recovery-box">
                  <div class="recovery-title">② TermX 接入密钥（TermX 桌面客户端连接用）</div>
                  <div class="recovery-key">{{ setupResult?.termx_key }}</div>
                  <div class="recovery-hint">
                    在 TermX 客户端设置里：服务器地址填 <code>{{ syncUrl }}</code>，密钥粘贴上面这串。
                    不使用 TermX 客户端可忽略；以后可随时在服务器上用
                    <code>./termx-server --show-key</code> 再查看
                  </div>
                  <div class="recovery-actions">
                    <el-button size="small" @click="onCopyTermxKey">
                      <el-icon style="margin-right: 4px"><CopyDocument /></el-icon>复制密钥
                    </el-button>
                  </div>
                </div>
                <div class="note">
                  <el-icon><InfoFilled /></el-icon>
                  <span>
                    数据主密钥已由系统自动生成并加密保管（忘记密码可用恢复密钥找回）；访问地址
                    <code>{{ setupResult?.access_url }}</code>
                  </span>
                </div>
              </template>
            </div>
          </transition>
        </div>

        <!-- 底部操作（固定高度，杜绝抖动） -->
        <footer class="footer">
          <el-button v-if="step >= 1 && step <= 4" text @click="step--">
            <el-icon style="margin-right: 4px"><ArrowLeft /></el-icon>
            上一步
          </el-button>
          <span v-else></span>

          <el-button
            v-if="step === 0"
            type="primary"
            size="large"
            round
            :disabled="!licenseAgreed"
            @click="step = 1"
          >
            开始配置
            <el-icon style="margin-left: 6px"><ArrowRight /></el-icon>
          </el-button>
          <el-button
            v-else-if="step === 1"
            type="primary"
            round
            :disabled="!dbStepOk"
            :loading="dbStepLoading"
            @click="dbStepNext"
          >
            {{ dbStepLoading ? '正在验证…' : '下一步' }}
            <el-icon v-if="!dbStepLoading" style="margin-left: 6px"><ArrowRight /></el-icon>
          </el-button>
          <el-button
            v-else-if="step === 2"
            type="primary"
            round
            :disabled="!svcStepOk"
            :loading="svcStepLoading"
            @click="svcStepNext"
          >
            {{ svcStepLoading ? '正在验证…' : '下一步' }}
            <el-icon v-if="!svcStepLoading" style="margin-left: 6px"><ArrowRight /></el-icon>
          </el-button>
          <el-button
            v-else-if="step === 3"
            type="primary"
            round
            :disabled="!secStepOk"
            @click="step = 4"
          >
            下一步
            <el-icon style="margin-left: 6px"><ArrowRight /></el-icon>
          </el-button>
          <el-button
            v-else-if="step === 4"
            type="primary"
            size="large"
            round
            :loading="initializing"
            @click="onFinish"
          >
            {{ initializing ? '正在初始化…' : '完成初始化' }}
          </el-button>
          <el-button
            v-else
            type="primary"
            size="large"
            round
            :disabled="!setupResult"
            :loading="goLoginBusy"
            @click="goLogin"
          >
            进入系统
            <el-icon style="margin-left: 6px"><ArrowRight /></el-icon>
          </el-button>
        </footer>
      </section>
    </div>

    <!-- 开源许可证弹窗 -->
    <el-dialog v-model="showLicense" title="TermX 开源许可证" width="680px">
      <div class="license-text">
        <p><b>TermX（开源版）</b>基于 MIT License 发布，你可以自由使用、复制、修改、合并、发布、分发、再许可及销售本软件。</p>
        <p>任何使用中须保留本版权声明与许可声明。作者不对使用后果承担责任。</p>
        <p></p>
        <pre>TermX 开源许可证（版本 1.0）

版权所有 (c) 2026 TermX 保留所有权利

━━ 第一章 定义 ━━
"本软件"：TermX 项目全部源代码与资源
"衍生作品"：基于本软件修改/重组/扩展的任何作品
"商业使用"：以营利为目的的任何使用行为

━━ 第二章 许可授予 ━━
✅ 查看、阅读、分析全部源代码
✅ 非商业环境中编译、构建、运行
✅ 学习目的的技术分析与研究
✅ 个人或团队内部非商业部署

━━ 第三章 限制与禁止 ━━
⛔ 禁止未经授权的二次开发（修改/fork/衍生须获书面同意）
⛔ 禁止任何商业使用（出售/托管收费/捆绑/有偿服务等）
⛔ 禁止删除版权声明与许可证标识
⛔ 禁止恶意使用（入侵/恶意软件/侵犯隐私等）
⛔ 禁止未经授权的再分发

━━ 第四章 知识产权 ━━
全部知识产权归作者所有，本协议仅授予使用权。

━━ 第五章 免责与责任限制 ━━
· 本软件按"现状"提供，不附带任何保证
· 作者不对数据丢失、利润损失、业务中断承担责任
· 最大责任上限为零（本软件免费提供）
· 使用风险由用户自行承担
· 作者不参与、不控制、不知晓用户的具体使用场景
· 用户对自身使用行为独立承担全部法律责任

━━ 第六章 终止 ━━
违反任何条款时本协议自动终止，须立即停止使用并删除。

适用法律：中华人民共和国法律
争议解决：协商 → 作者所在地人民法院

━━ 本协议自发布之日起生效 ━━</pre>
      </div>
      <template #footer>
        <el-button @click="showLicense = false">关闭</el-button>
        <el-button type="primary" @click="licenseAgreed = true; showLicense = false">
          我同意
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
/* ===== 页面与背景 ===== */
.setup-page {
  position: relative;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 28px 20px;
  overflow: hidden;
  background:
    radial-gradient(circle at 20% 18%, rgba(30, 111, 224, 0.08), transparent 42%),
    radial-gradient(circle at 85% 80%, rgba(62, 207, 142, 0.07), transparent 40%),
    var(--ky-bg);
}
.setup-page::before {
  content: '';
  position: absolute;
  inset: 0;
  background-image: radial-gradient(#d3ddec 1px, transparent 1px);
  background-size: 24px 24px;
  opacity: 0.55;
  pointer-events: none;
}

.glow {
  position: absolute;
  border-radius: 50%;
  filter: blur(80px);
  opacity: 0.45;
  pointer-events: none;
}
.glow-a {
  width: 460px;
  height: 460px;
  top: -160px;
  left: -120px;
  background: #7fb3f2;
}
.glow-b {
  width: 420px;
  height: 420px;
  bottom: -180px;
  right: -100px;
  background: #8fe6c4;
}

/* ===== 向导卡片 ===== */
.wizard {
  position: relative;
  z-index: 1;
  display: flex;
  width: 100%;
  max-width: 940px;
  min-height: 640px;
  background: #fff;
  border-radius: 22px;
  overflow: hidden;
  box-shadow:
    0 30px 70px rgba(15, 38, 74, 0.18),
    0 4px 16px rgba(15, 38, 74, 0.07);
  animation: rise 0.55s var(--ease-out) both;
}
@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(18px) scale(0.985);
  }
}

/* ===== 左侧品牌栏 ===== */
.side {
  position: relative;
  width: 272px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  padding: 32px 26px 26px;
  color: #fff;
  background: var(--ky-side-grad);
  overflow: hidden;
}
.side-deco {
  position: absolute;
  top: -90px;
  right: -120px;
  width: 320px;
  height: 320px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(101, 178, 255, 0.5), transparent 62%);
  pointer-events: none;
}
.side-deco::after {
  content: '';
  position: absolute;
  left: 10px;
  bottom: -150px;
  width: 300px;
  height: 300px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(45, 212, 191, 0.3), transparent 60%);
}

.side-brand {
  position: relative;
}
.brand-logo {
  width: 46px;
  height: 46px;
  display: block;
  border-radius: 12px;
  box-shadow: 0 8px 22px rgba(23, 58, 105, 0.55);
}
.brand-name {
  margin-top: 14px;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 1px;
}
.brand-sub {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.6;
  color: rgba(255, 255, 255, 0.72);
}

/* 垂直步骤 */
.side-steps {
  list-style: none;
  margin: 30px 0 0;
  padding: 0;
}
.side-steps li {
  position: relative;
  display: flex;
  gap: 14px;
  padding-bottom: 24px;
}
.side-steps li:not(:last-child)::before {
  content: '';
  position: absolute;
  left: 14px;
  top: 30px;
  bottom: 2px;
  width: 2px;
  border-radius: 1px;
  background: rgba(255, 255, 255, 0.25);
  transition: background 0.4s;
}
.side-steps li.done:not(:last-child)::before {
  background: var(--ky-accent);
}

.step-dot {
  width: 29px;
  height: 29px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  font-size: 13px;
  font-weight: 600;
  transition: all 0.4s var(--ease-out);
}
li.upcoming .step-dot {
  border: 1.5px dashed rgba(255, 255, 255, 0.5);
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.85);
}
li.active .step-dot {
  background: #fff;
  color: var(--ky-primary);
  box-shadow:
    0 0 0 5px rgba(255, 255, 255, 0.22),
    0 0 18px rgba(255, 255, 255, 0.35);
}
li.done .step-dot {
  background: var(--ky-accent);
  color: #06281a;
  border: none;
}

.step-info {
  padding-top: 4px;
}
.step-title {
  font-size: 14px;
  font-weight: 600;
  transition: color 0.3s;
}
li.upcoming .step-title {
  color: rgba(255, 255, 255, 0.68);
}
.step-desc {
  margin-top: 3px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.58);
}

.side-foot {
  margin-top: auto;
  position: relative;
  padding-top: 18px;
  border-top: 1px solid rgba(255, 255, 255, 0.2);
}
.foot-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: rgba(255, 255, 255, 0.8);
}
.foot-ver {
  margin-top: 8px;
  font-size: 11px;
  color: rgba(255, 255, 255, 0.6);
}

/* ===== 右侧内容区 ===== */
.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  padding: 30px 36px 22px;
  min-width: 0;
}

/* 固定高度内容区：切换步骤时卡片尺寸恒定，不再抖动 */
.content {
  height: 566px;
  position: relative;
  overflow: hidden auto;
}

.pane {
  height: 100%;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  text-align: center;
}
/* 顶部对齐类步骤页：内容上下居中；内容超高时自动回退为顶部对齐避免裁切 */
.pane-top {
  align-items: stretch;
  text-align: left;
  justify-content: safe center;
}

.pane h2 {
  margin: 0 0 6px;
  font-size: 21px;
  letter-spacing: 0.2px;
  position: relative;
  padding-left: 14px;
}
.pane h2::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 5px;
  height: 19px;
  border-radius: 3px;
  background: var(--ky-gradient);
}
.pane-sub {
  margin: 0 0 14px;
  color: var(--ky-text-sub);
  font-size: 14px;
}

/* 步骤切换过渡：滑动 + 淡入淡出 */
.step-enter-active,
.step-leave-active {
  transition: opacity 0.34s var(--ease-out), transform 0.34s var(--ease-out);
}
.step-enter-from {
  opacity: 0;
  transform: translateX(30px);
}
.step-leave-to {
  opacity: 0;
  transform: translateX(-22px);
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
.pop-enter-active {
  transition: transform 0.32s var(--ease-out), opacity 0.25s;
}
.pop-enter-from {
  transform: scale(0.3);
  opacity: 0;
}

/* 欢迎 */
.hero-icon {
  width: 74px;
  height: 74px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 20px;
  color: #fff;
  background: var(--ky-gradient);
  box-shadow: 0 14px 30px rgba(30, 111, 224, 0.38);
  animation: floaty 3.2s ease-in-out infinite;
}
@keyframes floaty {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-7px);
  }
}

.feature-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 14px;
  width: 100%;
}
.feature {
  padding: 20px 16px;
  border: 1px solid var(--ky-border);
  border-radius: var(--ky-radius);
  background: #fafcff;
  transition: transform 0.25s var(--ease-out), box-shadow 0.25s;
}
.feature:hover {
  transform: translateY(-3px);
  box-shadow: 0 10px 24px rgba(15, 38, 74, 0.09);
}
.feature-icon {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 11px;
  margin: 0 auto 12px;
  color: #fff;
  background: var(--ky-gradient);
}
.feature-title {
  font-size: 14px;
  font-weight: 600;
}
.feature-desc {
  margin-top: 6px;
  font-size: 12px;
  color: var(--ky-text-faint);
  line-height: 1.6;
}

/* 欢迎页：开源许可 */
.license-row {
  margin-top: 18px;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 13px;
}

/* 数据库选择卡片 */
.option-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}
.option-card {
  position: relative;
  border: 1.5px solid var(--ky-border);
  border-radius: var(--ky-radius);
  padding: 14px 16px 10px;
  cursor: pointer;
  background: #fff;
  transition:
    border-color 0.3s,
    box-shadow 0.3s,
    background 0.3s,
    transform 0.3s var(--ease-out);
}
.option-card:hover {
  border-color: var(--el-color-primary-light-5);
  transform: translateY(-2px);
}
.option-card.active {
  border-color: var(--ky-primary);
  background: var(--ky-primary-soft);
  box-shadow: 0 0 0 3.5px rgba(30, 111, 224, 0.13);
}
.option-check {
  position: absolute;
  top: 12px;
  right: 12px;
  font-size: 21px;
  color: var(--ky-primary);
}
.option-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-right: 26px;
}
.icon-sq {
  width: 38px;
  height: 38px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 11px;
  color: #fff;
}
.icon-sqlite {
  background: var(--ky-gradient);
}
.icon-ext {
  background: linear-gradient(135deg, #64748b 0%, #3f4d60 100%);
}
.option-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
}
.option-desc {
  margin-top: 2px;
  font-size: 12.5px;
  color: var(--ky-text-sub);
}
.option-points {
  list-style: none;
  margin: 10px 0 0;
  padding: 10px 0 0;
  border-top: 1px dashed var(--ky-border);
  display: flex;
  flex-direction: column;
  gap: 5px;
}
.option-points li {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  color: var(--ky-text-sub);
}
.option-points .el-icon {
  font-size: 12px;
  color: var(--ky-accent);
  flex-shrink: 0;
}

/* 平滑展开/收起（grid 高度过渡） */
.expand {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  margin-top: 0;
  transition:
    grid-template-rows 0.5s var(--ease-out),
    opacity 0.42s ease 0.06s,
    margin-top 0.5s var(--ease-out);
}
.expand.open {
  grid-template-rows: 1fr;
  opacity: 1;
  margin-top: 4px;
}
.expand-inner {
  overflow: hidden;
  min-height: 0;
}

/* 本地模式说明面板 */
.info-panel {
  padding: 14px 18px 12px;
  border: 1px solid #dbe7f4;
  border-radius: var(--ky-radius);
  background: linear-gradient(180deg, #f8fbff 0%, #f0f6fd 100%);
  box-shadow: 0 4px 18px rgba(15, 38, 74, 0.05);
}
.info-title {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 600;
  color: var(--ky-primary-deep);
}
.info-rows {
  margin-top: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.info-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12.5px;
  color: var(--ky-text-sub);
}
.info-row .el-icon {
  flex-shrink: 0;
}

code {
  padding: 2px 7px;
  border-radius: 6px;
  background: #eef3fb;
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  color: var(--ky-primary-deep);
}

/* 外部数据库连接面板 */
.conn-panel {
  padding: 12px 18px 8px;
  border: 1px solid #dbe7f4;
  border-radius: var(--ky-radius);
  background: linear-gradient(180deg, #f8fbff 0%, #f0f6fd 100%);
  box-shadow: 0 4px 18px rgba(15, 38, 74, 0.05);
}

/* 数据库类型胶囊选择 */
.engine-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}
.engine-label {
  font-size: 12.5px;
  color: var(--ky-text-faint);
}
.engine-pills {
  display: flex;
  gap: 6px;
}
.engine-pill {
  padding: 5px 14px;
  border: 1px solid var(--ky-border);
  border-radius: 999px;
  background: #fff;
  color: var(--ky-text-sub);
  font-size: 12.5px;
  cursor: pointer;
  transition: all 0.25s var(--ease-out);
}
.engine-pill:hover {
  border-color: var(--el-color-primary-light-5);
  color: var(--ky-primary);
}
.engine-pill.on {
  background: var(--ky-gradient);
  border-color: transparent;
  color: #fff;
  font-weight: 600;
  box-shadow: 0 4px 12px rgba(30, 111, 224, 0.3);
}

.conn-form :deep(.el-form-item) {
  margin-bottom: 5px;
}
.test-result-slot {
  height: 36px;
  margin-top: 4px;
  display: flex;
  align-items: stretch;
  overflow: hidden;
}
.test-result-slot :deep(.el-alert) {
  width: 100%;
  height: 100%;
  padding: 5px 14px;
}
.test-result-slot :deep(.el-alert__title) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 18px;
}
.form-row-end {
  grid-template-columns: 1fr auto;
  align-items: end;
}
.form-row-end .el-form-item {
  margin-bottom: 0;
}
.test-btn {
  height: 32px;
  margin-bottom: 5px;
}
.test-hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--ky-text-faint);
  text-align: center;
}

/* 服务/安全设置面板 */
.svc-panel {
  padding: 16px 20px 14px;
  border: 1px solid #dbe7f4;
  border-radius: var(--ky-radius);
  background: linear-gradient(180deg, #f8fbff 0%, #f0f6fd 100%);
  box-shadow: 0 4px 18px rgba(15, 38, 74, 0.05);
}
.svc-form :deep(.el-form-item) {
  margin-bottom: 8px;
}

/* 同步接口设置 */
.sync-row {
  margin-top: 4px;
  padding-top: 10px;
  border-top: 1px dashed var(--ky-border);
}
.sync-toggle {
  display: flex;
  align-items: center;
  gap: 10px;
}
.sync-state {
  font-size: 13px;
  font-weight: 600;
  color: var(--ky-text-faint);
}
.sync-state.on {
  color: #2e8f6d;
}
.sync-hint {
  font-size: 12px;
  color: var(--ky-text-faint);
}
.sync-url-row {
  display: flex;
  gap: 8px;
  width: 100%;
}
.sync-path-input {
  flex: 1;
}
.sync-path-input :deep(input) {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 13px;
  letter-spacing: 0.5px;
}
.sync-url-pill {
  margin-top: 8px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 5px 14px;
  border-radius: 999px;
  background: #eaf3fd;
  border: 1px solid #cde2f9;
  color: var(--ky-primary-deep);
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12.5px;
  font-weight: 600;
}
.sync-url-pill .el-icon {
  color: var(--ky-primary);
}
.sync-url-tip {
  margin-top: 5px;
  font-size: 12px;
  color: var(--el-color-danger);
}

/* 服务配置验证行 */
.srv-test-row {
  margin-top: 6px;
  padding-top: 8px;
  border-top: 1px dashed var(--ky-border);
}
.srv-test-hint {
  font-size: 12px;
  color: var(--ky-text-faint);
}

/* 弱密钥提示 */
.key-weak {
  color: #e08c2f;
  text-align: left;
}
.key-confirm-area {
  margin-top: 8px;
}

/* 初始化阶段清单 */
.phase-panel {
  padding: 14px 20px;
}
.phase-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 0;
  font-size: 13.5px;
  color: var(--ky-text-faint);
}
.phase-row.done {
  color: var(--ky-text-main);
}
.phase-dot {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  font-size: 12px;
  border: 1.5px dashed var(--ky-border);
  color: var(--ky-text-faint);
}
.phase-row.done .phase-dot {
  border: none;
  background: var(--ky-accent);
  color: #06281a;
}
.phase-row.active {
  color: var(--ky-primary);
  font-weight: 600;
}
.phase-row.active .phase-dot {
  border: 1.5px solid var(--ky-primary);
  color: var(--ky-primary);
}
.spin {
  animation: phase-spin 1s linear infinite;
}
@keyframes phase-spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}

/* 恢复密钥展示框 */
.recovery-box {
  margin-top: 10px;
  padding: 14px 16px;
  border: 1px solid #dbe7f4;
  border-radius: var(--ky-radius);
  background: #f8fbff;
}
.recovery-key {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 1px;
  word-break: break-all;
  color: var(--ky-primary-deep);
  user-select: all;
}
.recovery-title {
  font-size: 13px;
  font-weight: 700;
  color: var(--ky-primary-deep);
  margin-bottom: 6px;
}
.recovery-hint {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ky-text-faint);
}
.recovery-actions {
  margin-top: 10px;
  display: flex;
  gap: 8px;
}
.url-row {
  align-items: center;
}
.url-pill {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 5px 14px;
  border-radius: 999px;
  background: #eaf3fd;
  border: 1px solid #cde2f9;
  color: var(--ky-primary-deep);
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12.5px;
  font-weight: 600;
}
.svc-caption {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ky-text-faint);
}
.svc-caption .el-icon {
  margin-top: 2px;
  flex-shrink: 0;
  color: var(--ky-primary);
}

/* 密码强度 */
.strength {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: -2px 0 8px;
}
.strength-bars {
  display: flex;
  gap: 5px;
  flex: 1;
}
.strength-bars span {
  height: 5px;
  flex: 1;
  border-radius: 3px;
  background: #e3eaf4;
  transition: background 0.35s;
}
.strength-bars span.on[data-lv='1'] {
  background: #f56c6c;
}
.strength-bars span.on[data-lv='2'] {
  background: #e6a23c;
}
.strength-bars span.on[data-lv='3'] {
  background: #79c258;
}
.strength-bars span.on[data-lv='4'] {
  background: var(--ky-accent);
}
.strength-text {
  font-size: 12px;
  color: var(--ky-text-faint);
  flex-shrink: 0;
}
.strength-text[data-lv='1'] {
  color: #f56c6c;
}
.strength-text[data-lv='2'] {
  color: #e6a23c;
}
.strength-text[data-lv='3'],
.strength-text[data-lv='4'] {
  color: #2e8f6d;
}

.mismatch {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
  font-size: 12.5px;
  color: #f56c6c;
}

.warn-note {
  display: flex;
  align-items: flex-start;
  gap: 9px;
  margin-top: 4px;
  padding: 10px 15px;
  border-radius: 10px;
  background: #fdf6ec;
  border: 1px solid #f5e2c0;
  color: #a1642f;
  font-size: 12.5px;
  line-height: 1.7;
}
.warn-note .el-icon {
  margin-top: 3px;
  flex-shrink: 0;
  color: #e6a23c;
}
.warn-note b {
  color: #c2561a;
}

/* 密钥设置（干净专业风） */
.key-panel {
  padding: 16px 20px 14px;
}
.key-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}
.key-head-left {
  display: flex;
  align-items: center;
  gap: 10px;
}
.key-head-icon {
  width: 34px;
  height: 34px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 10px;
  color: #fff;
  background: var(--ky-gradient);
}
.key-head-title {
  font-size: 14px;
  font-weight: 700;
  color: var(--ky-text-main);
}
.key-head-sub {
  margin-top: 1px;
  font-size: 11.5px;
  color: var(--ky-text-faint);
}

/* 多行密钥输入框 */
.key-area :deep(textarea) {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 1px;
  line-height: 1.9;
  word-break: break-all;
}
.key-area :deep(textarea)::placeholder {
  letter-spacing: 0;
  font-weight: 400;
  font-size: 12.5px;
}
.key-count-row {
  margin-top: 8px;
  text-align: right;
  font-size: 12px;
  color: var(--ky-text-faint);
}
.key-count-row .ok {
  color: #2e8f6d;
}

/* 生成操作行 */
.key-action {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 14px 0;
  flex-wrap: wrap;
}
.key-action-hint {
  font-size: 12.5px;
  color: var(--ky-text-faint);
}

/* 安全设置：上下排列 + 提示瓦片 */
.sec-tiles {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-top: 14px;
}
.sec-tile {
  padding: 13px 14px;
  border-radius: 12px;
  background: #f7fbff;
  border: 1px solid var(--ky-border);
  text-align: left;
  transition: transform 0.22s var(--ease-out), box-shadow 0.22s;
}
.sec-tile:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 18px rgba(15, 38, 74, 0.08);
}
.tile-icon {
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 9px;
  color: #fff;
  margin-bottom: 8px;
}
.tile-icon.icon-blue {
  background: var(--ky-gradient);
}
.tile-icon.icon-green {
  background: linear-gradient(135deg, #52d69a 0%, #2fa772 100%);
}
.tile-icon.icon-orange {
  background: linear-gradient(135deg, #f5b860 0%, #e08c2f 100%);
}
.tile-title {
  font-size: 13px;
  font-weight: 700;
}
.tile-desc {
  margin-top: 4px;
  font-size: 12px;
  color: var(--ky-text-sub);
  line-height: 1.65;
}
.sec-tile.tile-warn {
  background: #fdf9f2;
  border-color: #f0e0c4;
}
.sec-tile.tile-warn .tile-title {
  color: #a1642f;
}
.field-err {
  margin-top: 5px;
  font-size: 12px;
  color: var(--el-color-danger);
}

/* 确认页：模式横幅 + 信息卡片 */
.mode-banner {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 13px 20px;
  border-radius: var(--ky-radius);
  background: linear-gradient(120deg, #eaf3fd 0%, #f6fbff 100%);
  border: 1px solid #d9e9fa;
}
.mode-icon {
  width: 50px;
  height: 50px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 13px;
  color: #fff;
  background: var(--ky-gradient);
  box-shadow: 0 8px 18px rgba(30, 111, 224, 0.3);
}
.mode-name {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 700;
}
.mode-desc {
  margin-top: 4px;
  font-size: 12.5px;
  color: var(--ky-text-sub);
}

.confirm-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
  margin-top: 12px;
}
.confirm-item {
  padding: 10px 15px;
  border: 1px solid var(--ky-border);
  border-radius: 12px;
  background: #fafcff;
  transition: border-color 0.25s;
}
.confirm-item:hover {
  border-color: var(--el-color-primary-light-5);
}
.confirm-label {
  font-size: 11.5px;
  color: var(--ky-text-faint);
  letter-spacing: 0.5px;
}
.confirm-value {
  margin-top: 4px;
  font-size: 13.5px;
  font-weight: 600;
}

.note {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-top: 12px;
  padding: 10px 15px;
  border-radius: 10px;
  background: #eef9f4;
  border: 1px solid #d3f0e4;
  color: #2e8f6d;
  font-size: 12.5px;
}

/* 完成 */
.done-icon-wrap {
  position: relative;
  width: 96px;
  height: 96px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 20px;
}
.done-icon {
  animation: pop-in 0.55s var(--ease-out) 0.1s both;
}
@keyframes pop-in {
  from {
    transform: scale(0.35);
    opacity: 0;
  }
}
.done-ring {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  border: 2.5px solid rgba(62, 207, 142, 0.45);
  animation: ripple 1.7s ease-out infinite;
}
@keyframes ripple {
  0% {
    transform: scale(0.72);
    opacity: 0.9;
  }
  100% {
    transform: scale(1.35);
    opacity: 0;
  }
}
.done-sub {
  margin-bottom: 18px;
}

.done-info {
  width: 100%;
  max-width: 460px;
  padding: 6px 20px;
  border: 1px solid var(--ky-border);
  border-radius: var(--ky-radius);
  background: #fafcff;
  text-align: left;
}
.done-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 0;
  font-size: 13px;
}
.done-row + .done-row {
  border-top: 1px dashed var(--ky-border);
}
.done-row .el-icon {
  color: var(--ky-primary);
  flex-shrink: 0;
}
.done-label {
  width: 64px;
  flex-shrink: 0;
  color: var(--ky-text-faint);
  font-size: 12.5px;
}
.done-hint {
  color: var(--ky-text-faint);
  font-size: 12px;
}
.done-row b {
  color: #c2561a;
}

/* 许可证弹窗 */
.license-text {
  font-size: 13px;
  line-height: 1.8;
  color: var(--ky-text-main);
}
.license-text pre {
  margin: 12px 0 0;
  padding: 12px 14px;
  border-radius: 10px;
  background: #f5f8fc;
  font-size: 11.5px;
  line-height: 1.6;
  overflow: auto;
  max-height: 240px;
  color: var(--ky-text-sub);
}

/* 底部 */
.footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 62px;
  margin-top: 10px;
  padding-top: 14px;
  border-top: 1px solid var(--ky-border);
}
.footer :deep(.el-button--primary:not(.is-disabled)) {
  background: var(--ky-gradient);
  border: none;
  box-shadow: 0 8px 20px rgba(30, 111, 224, 0.32);
  transition: filter 0.25s, transform 0.25s var(--ease-out);
}
.footer :deep(.el-button--primary:not(.is-disabled):hover) {
  filter: brightness(1.07);
  transform: translateY(-1px);
}

/* 响应式：窄屏时侧栏变顶部横条 */
@media (max-width: 880px) {
  .wizard {
    flex-direction: column;
    min-height: 0;
  }
  .side {
    width: 100%;
    padding: 22px 24px 18px;
  }
  .side-steps {
    display: none;
  }
  .side-foot {
    display: none;
  }
  .brand-sub {
    margin-top: 4px;
  }
  .content {
    height: auto;
    min-height: 460px;
  }
  .feature-grid,
  .option-grid,
  .form-row,
  .confirm-grid,
  .sec-tiles {
    grid-template-columns: 1fr;
  }
  .engine-row {
    flex-wrap: wrap;
  }
}

@media (max-width: 480px) {
  .setup-page {
    padding: 14px 10px;
  }
  .main {
    padding: 20px 16px 16px;
  }
  .footer {
    height: auto;
    flex-wrap: wrap;
    gap: 8px;
  }
  .footer :deep(.el-button) {
    flex: 1;
    margin-left: 0;
  }
  .pane h2 {
    font-size: 19px;
  }
  .url-pill {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
</style>
