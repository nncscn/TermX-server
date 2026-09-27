<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiChangePassword, apiGenerateRecoveryKey } from '@/api/backend'
import { apiExportData, apiImportData } from '@/api/data'
import {
  apiGetDbConfig,
  apiGetSettings,
  apiRevertDb,
  apiSaveDb,
  apiTestDb,
  apiUpdateSettings
, apiCheckPort } from '@/api/settings'
import { copyText } from '@/utils/format'
import { getSessionDataKey } from '@/utils/crypto'
import { applyTrashRetention, getTrashRetention } from '@/utils/retention'
import logoUrl from '@/assets/logo.png'

// ---------- 修改主密码 ----------
const pwd = reactive({ current: '', next: '', confirm: '' })
const changing = ref(false)

const pwdMismatch = computed(() => pwd.confirm.length > 0 && pwd.next !== pwd.confirm)

const strengthLevel = computed(() => {
  const p = pwd.next
  if (!p) return 0
  let score = 1
  if (p.length >= 8) score++
  if (p.length >= 12) score++
  if (/[a-z]/.test(p) && /[A-Z]/.test(p)) score++
  if (/\d/.test(p) && /[^a-zA-Z0-9]/.test(p)) score++
  return Math.min(score, 4)
})
const strengthText = computed(() => ['未设置', '弱', '一般', '较强', '强'][strengthLevel.value])

const pwdOk = computed(
  () => pwd.current.length >= 8 && pwd.next.length >= 8 && !pwdMismatch.value
)

async function onChangePassword() {
  if (!pwdOk.value) return
  changing.value = true
  try {
    await apiChangePassword(pwd.current, pwd.next)
    ElMessage.success('主密码已修改，下次解锁请使用新密码')
    Object.assign(pwd, { current: '', next: '', confirm: '' })
  } catch (e) {
    ElMessage.error(e.message)
  } finally {
    changing.value = false
  }
}

// 恢复密钥：服务端只存哈希，明文仅生成时返回一次
async function onRegenRecovery() {
  try {
    await ElMessageBox.confirm(
      '重新生成后旧恢复密钥立即失效，新密钥仅展示一次。确定继续？',
      '重新生成恢复密钥',
      { type: 'warning', confirmButtonText: '生成', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  let masterPassword
  try {
    masterPassword = await promptMasterPassword('重新生成恢复密钥需要验证主密码（用于解包数据密钥）')
  } catch {
    return
  }
  if (!masterPassword) {
    ElMessage.warning('请输入主密码')
    return
  }
  try {
    const data = await apiGenerateRecoveryKey(masterPassword)
    await ElMessageBox.alert(
      `<div style="font-family:monospace;font-size:14px;letter-spacing:1px;word-break:break-all;padding:12px;border:1px dashed #b9d3f0;border-radius:8px;background:#f5f8fc;margin:10px 0">${data.recovery_key}</div><div style="font-size:12px;color:#e08c2f">仅此一次展示，请立即抄写或复制保存</div>`,
      '新恢复密钥',
      { dangerouslyUseHTMLString: true, confirmButtonText: '我已保存' }
    )
  } catch (e) {
    ElMessage.error(e.message)
  }
}

// ---------- 服务连接 ----------
const apiBase = ref(localStorage.getItem('ky_api_base') || 'http://192.168.61.127:8080')
const apiScheme = ref(localStorage.getItem('ky_api_scheme') || 'http')
const certPath = ref(localStorage.getItem('ky_cert_path') || '')
const certKey = ref(localStorage.getItem('ky_cert_key') || '')
const testing = ref(false)

const connPreview = computed(
  () => `${apiScheme.value}://${apiBase.value.replace(/^https?:\/\//, '')}`
)

const connOk = ref(null) // null=未测试 true/false=最近一次结果

// ---------- 端口占用检测（服务器本机端口，非前端可达性） ----------
const checkPort = ref(18080)
const checkingPort = ref(false)
const portResult = ref(null) // null=未测 | {occupied, message}

async function onCheckPort() {
  if (!checkPort.value || checkPort.value < 1 || checkPort.value > 65535) {
    ElMessage.warning('请输入 1-65535 之间的端口')
    return
  }
  checkingPort.value = true
  portResult.value = null
  try {
    portResult.value = await apiCheckPort(checkPort.value)
  } catch (e) {
    ElMessage.error(e.message || '检测失败')
  } finally {
    checkingPort.value = false
  }
}

/** 对给定地址做真实健康检查并联动状态点，返回 {ok}（不弹提示，由调用方决定话术） */
async function testConn(scheme, host) {
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), 5000)
  try {
    const resp = await fetch(`${scheme}://${host}/api/v1/health`, { signal: ctrl.signal })
    clearTimeout(timer)
    const payload = await resp.json().catch(() => null)
    const ok = !!(payload && payload.code === 0)
    connOk.value = ok
    return { ok }
  } catch {
    clearTimeout(timer)
    connOk.value = false
    return { ok: false }
  }
}

async function onTestConn() {
  testing.value = true
  const host = apiBase.value.trim().replace(/^https?:\/\//, '')
  const { ok } = await testConn(apiScheme.value, host)
  if (ok) ElMessage.success(`连接成功：${apiScheme.value}://${host} 服务运行正常`)
  else ElMessage.error('连接失败：无法访问后端服务，请检查地址与协议')
  testing.value = false
}

const savingConn = ref(false)
async function saveConn() {
  const host = apiBase.value.trim().replace(/^https?:\/\//, '') || '192.168.61.127:8080'
  apiBase.value = host
  const changed =
    localStorage.getItem('ky_api_base') !== host ||
    localStorage.getItem('ky_api_scheme') !== apiScheme.value
  savingConn.value = true
  localStorage.setItem('ky_api_base', host)
  localStorage.setItem('ky_api_scheme', apiScheme.value)
  localStorage.setItem('ky_cert_path', certPath.value.trim())
  localStorage.setItem('ky_cert_key', certKey.value.trim())
  // 保存即测试：用刚保存的地址做真实健康检查，状态点立即联动
  const { ok } = await testConn(apiScheme.value, host)
  if (ok) {
    ElMessage.success(`已保存，连接正常（${apiScheme.value}://${host}）`)
    if (changed) {
      ElMessage.info('后端地址已变更：后续请求立即走新地址；若指向不同服务器需重新登录')
    }
  } else {
    ElMessage.warning('设置已保存，但该地址当前无法访问，请检查地址与协议')
  }
  savingConn.value = false
}

// ---------- 数据库（服务端 config.yaml 真实读写：保存验主密码、自动建库、自动重启） ----------
const DB_PORTS = { mysql: 3306, postgresql: 5432, sqlserver: 1433 }
const DB_LABELS = { sqlite: 'SQLite（本地）', mysql: 'MySQL', postgresql: 'PostgreSQL', sqlserver: 'SQL Server' }
const db = reactive({
  engine: 'sqlite',
  host: '',
  port: 3306,
  user: '',
  password: '',
  name: 'keyvault',
  file: 'data/app.db'
})
const dbRunning = ref('sqlite') // 进程当前实际使用的引擎（该选项置灰不可选）
const dbPending = ref('') // 配置文件中待生效的引擎（保存后重启失败/未重启时出现）
const dbHasPass = ref(false)
const dbTesting = ref(false)
const dbSaving = ref(false)
const dbOk = ref(null)
const dbRestarting = ref(false)
const dbMigrate = ref(true) // 切换时迁移数据（结构 + 全部密钥/凭据/账户/偏好）
const dbPrivileges = ref({}) // 各引擎所需权限说明（服务端下发）

async function refreshDbConfig() {
  const cfg = await apiGetDbConfig()
  db.engine = cfg.engine || 'sqlite'
  db.file = cfg.file || 'data/app.db'
  db.host = cfg.host || ''
  db.port = cfg.port || DB_PORTS[db.engine] || 3306
  db.user = cfg.user || ''
  db.name = cfg.name || 'keyvault'
  db.password = ''
  dbHasPass.value = !!cfg.has_password
  dbRunning.value = cfg.running_engine || cfg.engine || 'sqlite'
  dbPending.value = cfg.pending_engine || ''
  dbPrivileges.value = cfg.privileges || {}
}

onMounted(async () => {
  try {
    await refreshDbConfig()
  } catch {
    /* 读取失败保持默认，保存前仍会强制测试 */
  }
})

function onDbEngine(e) {
  if (DB_PORTS[e]) db.port = DB_PORTS[e]
}

/** 请求体：sqlite 只带文件路径，外部引擎带连接参数；migrate_data 控制是否迁移数据 */
function dbReqBody() {
  const sqlite = db.engine === 'sqlite'
  return {
    engine: db.engine,
    host: sqlite ? '' : db.host.trim(),
    port: sqlite ? 0 : db.port,
    user: sqlite ? '' : db.user.trim(),
    password: sqlite ? '' : db.password,
    name: sqlite ? '' : db.name.trim(),
    file: sqlite ? db.file.trim() : '',
    migrate_data: dbMigrate.value
  }
}

async function onTestDb() {
  dbTesting.value = true
  try {
    const r = await apiTestDb(dbReqBody())
    dbOk.value = true
    ElMessage.success(r.message || '连接成功')
  } catch (e) {
    dbOk.value = false
    ElMessage.error(e.message || '连接失败')
  } finally {
    dbTesting.value = false
  }
}

const dbPreview = computed(() => {
  if (db.engine === 'sqlite') return `sqlite://./${db.file || 'data/app.db'}`
  const auth = db.user ? `${db.user}@` : ''
  return `${db.engine}://${auth}${db.host || '…'}:${db.port}/${db.name || '…'}`
})

/** 保存后等待服务自重启完成：轮询健康检查，并确认运行引擎已切换为目标引擎 */
async function waitRestartDone(targetEngine) {
  dbRestarting.value = true
  const scheme = localStorage.getItem('ky_api_scheme') || 'http'
  const host = (localStorage.getItem('ky_api_base') || 'http://192.168.61.127:8080').replace(/^https?:\/\//, '')
  for (let i = 0; i < 22; i++) {
    await new Promise((r) => setTimeout(r, 1200))
    try {
      const resp = await fetch(`${scheme}://${host}/api/v1/health`)
      const j = await resp.json()
      if (j && j.code === 0) {
        try {
          const cfg = await apiGetDbConfig()
          if ((cfg.running_engine || cfg.engine) === targetEngine) {
            dbRestarting.value = false
            await refreshDbConfig()
            return true
          }
        } catch {
          /* 新库无会话时该请求会 40101 整页跳登录，无需处理 */
        }
      }
    } catch {
      /* 重启窗口期连不上属正常，继续等 */
    }
  }
  dbRestarting.value = false
  return false
}

async function saveDb() {
  if (db.engine === dbRunning.value) {
    ElMessage.info('当前正在使用该数据库，无需切换')
    return
  }
  if (db.engine !== 'sqlite' && (!db.host.trim() || !db.name.trim())) {
    ElMessage.warning('请填写主机地址与数据库名')
    return
  }
  try {
    await ElMessageBox.confirm(
      dbMigrate.value
        ? `将切换到 ${DB_LABELS[db.engine] || db.engine} 并自动重启后端；当前库的结构与全部数据（密钥/凭据/账户/偏好）会一并迁移，账户密码与登录态保持不变。若目标库已存在会自动换用不冲突的新库名并弹窗告知。确定切换？`
        : `将切换到 ${DB_LABELS[db.engine] || db.engine} 并自动重启后端；已选择不迁移数据，新库为全新空库（账户将重新初始化）。确定切换？`,
      '切换数据库',
      { type: 'warning', confirmButtonText: '验证并切换', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  if (db.engine !== 'sqlite' && !db.password && !dbHasPass.value) {
    try {
      await ElMessageBox.confirm(
        '未填写数据库密码，将使用空密码连接并建库，确定继续？',
        '空密码确认',
        { type: 'warning', confirmButtonText: '继续', cancelButtonText: '返回填写' }
      )
    } catch {
      return
    }
  }
  let masterPassword
  try {
    masterPassword = await promptMasterPassword('切换数据库需要验证主密码')
  } catch {
    return
  }
  if (!masterPassword) {
    ElMessage.warning('请输入主密码')
    return
  }
  dbSaving.value = true
  try {
    await doSaveDb(masterPassword)
    dbSaving.value = false
    const ok = await waitRestartDone(db.engine)
    if (ok) {
      dbOk.value = true
      ElMessage.success('数据库已切换并重启完成')
    } else {
      ElMessage.warning('后端重启超时，请手动检查服务状态（data/config.yaml.bak 可用于回滚）')
    }
  } catch (e) {
    dbSaving.value = false
    ElMessage.error(e.message || '保存失败（验证未通过或连接失败）')
  }
}

/** 实际提交保存；目标库名冲突（40901）时弹窗征询，确认后带建议名重试一次 */
async function doSaveDb(masterPassword) {
  try {
    await apiSaveDb({ ...dbReqBody(), master_password: masterPassword })
    return
  } catch (e) {
    if (e.code === 40901 && e.data && e.data.suggested_name) {
      const req = e.data.requested_name || (db.engine === 'sqlite' ? db.file : db.name)
      let use
      try {
        await ElMessageBox.confirm(
          `目标库「${req}」已存在（可能含有数据，不会触碰）。是否改用自动避开的新库名「${e.data.suggested_name}」继续切换？`,
          '目标库同名',
          { type: 'warning', confirmButtonText: `使用 ${e.data.suggested_name}`, cancelButtonText: '取消切换' }
        )
        use = e.data.suggested_name
      } catch {
        throw new Error('已取消切换')
      }
      if (db.engine === 'sqlite') db.file = use
      else db.name = use
      await apiSaveDb({ ...dbReqBody(), master_password: masterPassword })
      ElMessage.success(`已改用新库名「${use}」完成保存`)
      return
    }
    throw e
  }
}

/** 撤销待生效变更（恢复为当前运行配置，不重启） */
async function revertDb() {
  let masterPassword
  try {
    masterPassword = await promptMasterPassword('撤销变更需要验证主密码')
  } catch {
    return
  }
  if (!masterPassword) {
    ElMessage.warning('请输入主密码')
    return
  }
  try {
    await apiRevertDb(masterPassword)
    await refreshDbConfig()
    ElMessage.success('已撤销待生效变更，恢复为当前运行配置')
  } catch (e) {
    ElMessage.error(e.message || '撤销失败')
  }
}

const dataKeyVisible = ref(false)
const dataKeyLen = computed(() => getSessionDataKey().length)
function toggleDataKey() {
  dataKeyVisible.value = !dataKeyVisible.value
}
async function copyDataKey() {
  const ok = await copyText(getSessionDataKey())
  ok ? ElMessage.success('数据密钥已复制') : ElMessage.error('复制失败')
}

// 启动页
const startPage = ref(localStorage.getItem('ky_start_page') || '/dashboard')
const startPageOptions = [
  { label: '仪表盘', value: '/dashboard' },
  { label: '连接凭据', value: '/credentials' }
]

// 剪贴板自动清除
const clipClear = ref(localStorage.getItem('ky_clipboard_clear') || '0')
const clipClearOptions = [
  { label: '关闭', value: '0' },
  { label: '10 秒', value: '10' },
  { label: '30 秒', value: '30' },
  { label: '60 秒', value: '60' }
]

const autoLock = ref(localStorage.getItem('ky_autolock') || '0')
const lockOptions = [
  { label: '从不', value: '0' },
  { label: '5 分钟', value: '5' },
  { label: '15 分钟', value: '15' },
  { label: '30 分钟', value: '30' }
]

const retention = ref(String(getTrashRetention()))
const retentionOptions = [
  { label: '关闭', value: '0' },
  { label: '7 天', value: '7' },
  { label: '30 天', value: '30' },
  { label: '90 天', value: '90' }
]

// 进入页面时以服务端为准回填（数据库是事实源）
onMounted(async () => {
  try {
    const p = await apiGetSettings()
    startPage.value = p.start_page
    clipClear.value = String(p.clipboard_clear)
    autoLock.value = String(p.autolock_minutes)
    retention.value = String(p.trash_retention_days)
  } catch (e) {
    ElMessage.error(e.message || '读取设置失败')
  }
})

/** 偏好整体提交（四项一起保存），成功后本地缓存由适配层自动同步 */
async function savePrefs() {
  try {
    await apiUpdateSettings({
      start_page: startPage.value,
      clipboard_clear: parseInt(clipClear.value, 10) || 0,
      autolock_minutes: parseInt(autoLock.value, 10) || 0,
      trash_retention_days: parseInt(retention.value, 10) || 0
    })
    return true
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
    return false
  }
}

function onStartPageChange(v) {
  startPage.value = v
  savePrefs().then((ok) => ok && ElMessage.success('已保存，下次解锁后首先打开该页面'))
}

function onClipClearChange(v) {
  clipClear.value = v
  savePrefs().then((ok) =>
    ok &&
    ElMessage.success(
      v === '0' ? '已关闭剪贴板自动清除' : `已保存，复制敏感信息 ${v} 秒后自动清空剪贴板`
    )
  )
}

function onLockChange(v) {
  autoLock.value = v
  savePrefs().then((ok) =>
    ok && ElMessage.success(v === '0' ? '已关闭自动锁定' : `已保存，空闲 ${v} 分钟后自动锁定`)
  )
}

function onRetentionChange(v) {
  retention.value = v
  savePrefs().then((ok) => {
    if (!ok) return
    applyTrashRetention().then((purged) => {
      ElMessage.success(purged > 0 ? `已保存，清理了 ${purged} 条过期记录` : '已保存')
    })
  })
}

/** 弹窗输入主密码（导出/导入备份、切换数据库、撤销变更的验证凭证） */
async function promptMasterPassword(message = '备份包含解密后的密钥内容，需要验证主密码后才能导出/导入') {
  const { value } = await ElMessageBox.prompt(message, '验证主密码', {
    inputType: 'password',
    inputPlaceholder: '主密码',
    confirmButtonText: '确定',
    cancelButtonText: '取消'
  })
  return value
}

const exporting = ref(false)
async function exportBackup() {
  let password
  try {
    password = await promptMasterPassword()
  } catch {
    return
  }
  if (!password) {
    ElMessage.warning('请输入主密码')
    return
  }
  exporting.value = true
  let data
  try {
    data = await apiExportData(password)
  } catch (e) {
    ElMessage.error(e.message || '导出失败')
    return
  } finally {
    exporting.value = false
  }
  const backup = {
    app: 'TermX',
    version: '0.1.0',
    exportedAt: new Date().toISOString(),
    keys: data.keys,
    credentials: data.credentials
  }
  const blob = new Blob([JSON.stringify(backup, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  const d = new Date()
  const p = (n) => String(n).padStart(2, '0')
  a.href = url
  a.download = `ky-backup-${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}.json`
  a.click()
  URL.revokeObjectURL(url)
  ElMessage.success('备份文件已生成并下载')
}

// 备份导入
const importInputRef = ref(null)
function pickImportFile() {
  importInputRef.value?.click()
}
function onImportFile(e) {
  const file = e.target.files?.[0]
  e.target.value = ''
  if (!file) return
  const reader = new FileReader()
  reader.onload = async () => {
    let data
    try {
      data = JSON.parse(reader.result)
    } catch {
      ElMessage.error('备份文件格式不正确')
      return
    }
    if (!Array.isArray(data.keys) || !Array.isArray(data.credentials)) {
      ElMessage.error('不是有效的TermX备份文件')
      return
    }
    try {
      await ElMessageBox.confirm(
        `将导入 ${data.keys.length} 条密钥、${data.credentials.length} 条凭据，并覆盖当前数据。确定继续？`,
        '恢复备份',
        { type: 'warning', confirmButtonText: '导入并覆盖', cancelButtonText: '取消' }
      )
    } catch {
      return
    }
    let password
    try {
      password = await promptMasterPassword()
    } catch {
      return
    }
    if (!password) {
      ElMessage.warning('请输入主密码')
      return
    }
    try {
      await apiImportData(password, data)
      ElMessage.success('备份已恢复，即将刷新页面')
      setTimeout(() => window.location.reload(), 800)
    } catch (err) {
      ElMessage.error(err.message || '导入失败')
    }
  }
  reader.readAsText(file)
}

// ---------- 关于 ----------
// TODO: 版本检查接口还没接,后面再加回来
const showLicense = ref(false)
async function copyTextOf(text, what) {
  const ok = await copyText(text || '')
  ok ? ElMessage.success(`${what}已复制`) : ElMessage.error('复制失败')
}

</script>

<template>
  <div class="page settings-page stagger">
    <!-- 修改主密码 -->
      <div class="card card-pad block">
      <div class="block-head">
        <div>
          <div class="block-title">修改主密码</div>
          <div class="block-desc">主密码用于解锁密钥库与数据加密，修改后立即生效</div>
        </div>
      </div>

      <el-form label-position="top" class="pwd-form">
        <el-form-item label="当前主密码" required>
          <el-input
            v-model="pwd.current"
            type="password"
            show-password
            placeholder="输入当前主密码"
          />
        </el-form-item>
        <div class="form-grid">
          <el-form-item label="新主密码" required>
            <el-input
              v-model="pwd.next"
              type="password"
              show-password
              placeholder="至少 8 位字符"
            />
          </el-form-item>
          <el-form-item label="确认新主密码" required>
            <el-input
              v-model="pwd.confirm"
              type="password"
              show-password
              placeholder="再次输入新主密码"
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
          <span class="strength-text" :data-lv="strengthLevel">强度：{{ strengthText }}</span>
        </div>
        <div class="mismatch" :style="{ visibility: pwdMismatch ? 'visible' : 'hidden' }">
          <el-icon><CircleCloseFilled /></el-icon>
          两次输入的新密码不一致
        </div>

        <div class="form-foot">
          <el-button
            type="primary"
            round
            :loading="changing"
            :disabled="!pwdOk"
            @click="onChangePassword"
          >
            修改主密码
          </el-button>
        </div>
      </el-form>
    </div>

    <!-- 服务连接 -->
    <div class="card card-pad block">
      <div class="block-head">
        <div>
          <div class="block-title">服务连接</div>
          <div class="block-desc">前端访问后端的地址与协议设置，保存后生效</div>
        </div>
      </div>
      <div class="pref-rows">
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">后端地址</div>
            <div class="pref-desc">前端 API 请求的目标地址（IP 或域名 + 端口）</div>
          </div>
          <div class="conn-ctrl">
            <el-input v-model="apiBase" placeholder="192.168.61.127:8080" class="set-input" />
          </div>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">连接协议</div>
            <div class="pref-desc">HTTPS 需要在服务端配置证书后启用</div>
          </div>
          <el-radio-group v-model="apiScheme" size="small">
            <el-radio-button value="http">HTTP</el-radio-button>
            <el-radio-button value="https">HTTPS</el-radio-button>
          </el-radio-group>
        </div>
        <div class="expand-box" :class="{ open: apiScheme === 'https' }">
          <div class="expand-inner">
            <div class="pref-row">
              <div class="pref-text">
                <div class="pref-label">证书路径</div>
                <div class="pref-desc">服务端 PEM 证书文件路径</div>
              </div>
              <el-input v-model="certPath" placeholder="/etc/ky/server.crt" class="set-input" />
            </div>
            <div class="pref-row">
              <div class="pref-text">
                <div class="pref-label">私钥路径</div>
                <div class="pref-desc">服务端 PEM 私钥文件路径</div>
              </div>
              <el-input v-model="certKey" placeholder="/etc/ky/server.key" class="set-input" />
            </div>
          </div>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">连接状态</div>
            <div class="pref-desc">测试前端到后端的连通性</div>
          </div>
          <div class="conn-ctrl">
            <span class="conn-state">
              <i :class="connOk === true ? 'dot-on' : 'dot-off'"></i>
              {{ connOk === true ? '连接正常' : connOk === false ? '连接失败' : '未测试' }}
            </span>
            <el-button type="primary" plain round size="small" :loading="testing" @click="onTestConn">测试连接</el-button>
          </div>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">端口占用检测</div>
            <div class="pref-desc">在服务器本机真实试绑该端口，检测是否被占用</div>
          </div>
          <div class="conn-ctrl">
            <el-input-number
              v-model="checkPort"
              :min="1"
              :max="65535"
              controls-position="right"
              size="small"
              style="width: 110px"
            />
            <el-button
              type="primary"
              plain
              round
              size="small"
              :loading="checkingPort"
              @click="onCheckPort"
            >检测端口</el-button>
            <span v-if="portResult" :class="portResult.occupied ? 'port-busy' : 'port-free'">
              {{ portResult.message }}
            </span>
          </div>
        </div>
      </div>
      <div class="conn-preview">
        <el-icon :size="13"><Link /></el-icon>
        <code>{{ connPreview }}</code>
      </div>
      <div class="form-foot">
        <el-button type="primary" round :loading="savingConn" @click="saveConn">保存连接设置</el-button>
      </div>
    </div>

    <!-- 数据库 -->
    <div class="card card-pad block">
      <div class="block-head">
        <div>
          <div class="block-title">数据库</div>
          <div class="block-desc">
            当前使用：{{ DB_LABELS[dbRunning] || dbRunning }}
            · 切换需验证主密码并自动重启；目标库不存在会自动创建
          </div>
        </div>
      </div>

      <!-- 待生效变更提示条 -->
      <div class="pending-banner" v-if="dbPending">
        <el-icon :size="15"><InfoFilled /></el-icon>
        <span>
          已有待生效变更：<b>{{ DB_LABELS[dbPending] || dbPending }}</b>（保存后未完成重启），
          重启后端后生效
        </span>
        <el-button size="small" round @click="revertDb">撤销变更</el-button>
      </div>

      <div class="pref-rows">
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">数据库引擎</div>
            <div class="pref-desc">本地模式零配置；外部数据库需填写连接信息</div>
          </div>
          <div class="db-engine-ctrl">
            <el-radio-group v-model="db.engine" size="small" @change="onDbEngine">
              <el-radio-button value="sqlite" :disabled="dbRunning === 'sqlite'">SQLite</el-radio-button>
              <el-radio-button value="mysql" :disabled="dbRunning === 'mysql'">MySQL</el-radio-button>
              <el-radio-button value="postgresql" :disabled="dbRunning === 'postgresql'">PostgreSQL</el-radio-button>
              <el-radio-button value="sqlserver" :disabled="dbRunning === 'sqlserver'">SQL Server</el-radio-button>
            </el-radio-group>
          </div>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">迁移数据</div>
            <div class="pref-desc">切换时把当前库的结构与全部数据搬到新库（密文直接搬运，账户密码与登录态保持不变）</div>
          </div>
          <el-switch v-model="dbMigrate" />
        </div>
        <div class="pref-row" v-if="db.engine !== 'sqlite'">
          <div class="pref-text">
            <div class="pref-label">所需权限</div>
            <div class="pref-desc">{{ dbPrivileges[db.engine] || '连接与建库权限' }}</div>
          </div>
        </div>

        <div class="expand-box" :class="{ open: db.engine === 'sqlite' }">
          <div class="expand-inner">
            <div class="pref-row">
              <div class="pref-text">
                <div class="pref-label">数据文件</div>
                <div class="pref-desc">本地单文件数据库，随数据目录保存</div>
              </div>
              <code class="db-file">{{ db.file }}</code>
            </div>
          </div>
        </div>

        <div class="expand-box" :class="{ open: db.engine !== 'sqlite' }">
          <div class="expand-inner">
          <div class="pref-row">
            <div class="pref-text">
              <div class="pref-label">主机地址</div>
              <div class="pref-desc">数据库服务器 IP 或域名</div>
            </div>
            <el-input v-model="db.host" placeholder="192.168.1.20" class="set-input" />
          </div>
          <div class="pref-row">
            <div class="pref-text">
              <div class="pref-label">端口</div>
              <div class="pref-desc">随引擎自动切换，可手动修改</div>
            </div>
            <el-input-number v-model="db.port" :min="1" :max="65535" controls-position="right" class="set-port" />
          </div>
          <div class="pref-row">
            <div class="pref-text">
              <div class="pref-label">用户名</div>
              <div class="pref-desc">数据库登录账号</div>
            </div>
            <el-input v-model="db.user" placeholder="ky_app" class="set-input" />
          </div>
          <div class="pref-row">
            <div class="pref-text">
              <div class="pref-label">密码</div>
              <div class="pref-desc">数据库登录密码，密文保存</div>
            </div>
            <el-input v-model="db.password" type="password" show-password class="set-input" />
          </div>
          <div class="pref-row">
            <div class="pref-text">
              <div class="pref-label">数据库名</div>
              <div class="pref-desc">业务库 schema 名称；保存时不存在会自动创建</div>
            </div>
            <el-input v-model="db.name" placeholder="keyvault" class="set-input" />
          </div>
          <div class="pref-row">
            <div class="pref-text">
              <div class="pref-label">连接状态</div>
              <div class="pref-desc">真实连通测试：由后端尝试连接目标数据库</div>
            </div>
            <div class="conn-ctrl">
              <span class="conn-state">
                <i :class="dbOk === true ? 'dot-on' : 'dot-off'"></i>
                {{ dbOk === true ? '连接正常' : dbOk === false ? '连接失败' : '未测试' }}
              </span>
              <el-button type="primary" plain round size="small" :loading="dbTesting" @click="onTestDb">测试连接</el-button>
            </div>
          </div>
          </div>
        </div>
      </div>
      <div class="conn-preview">
        <el-icon :size="13"><Coin /></el-icon>
        <code>{{ dbPreview }}</code>
      </div>
      <div class="form-foot">
        <el-button type="primary" round :loading="dbSaving" @click="saveDb">保存数据库设置</el-button>
      </div>
    </div>

    <!-- 数据与偏好 -->
      <div class="card card-pad block">
      <div class="block-head">
        <div>
          <div class="block-title">数据与偏好</div>
          <div class="block-desc">回收站保留、自动锁定与备份，仅保存在本机</div>
        </div>
      </div>
      <div class="pref-rows">
        <div class="pref-row" style="align-items: flex-start">
          <div class="pref-text">
            <div class="pref-label">数据密钥</div>
            <div class="pref-desc">
              本次会话的本地解密密钥（Base64 43 字符）。业务数据在浏览器内用它加密后入库，服务端不解开；
              编辑弹窗输入它即可解密。重新登录后自动更新。
            </div>
            <code v-if="dataKeyVisible && dataKeyLen" class="dk-code">{{ getSessionDataKey() }}</code>
            <div v-else-if="!dataKeyLen" class="pref-desc" style="color: #e08c2f; margin-top: 4px">
              本次会话未获取到数据密钥（重新登录即可获取）
            </div>
          </div>
          <div style="display: flex; gap: 8px; flex-direction: column; align-items: stretch">
            <el-button plain round size="small" :disabled="!dataKeyLen" @click="toggleDataKey">
              {{ dataKeyVisible ? '隐藏' : '查看' }}
            </el-button>
            <el-button type="primary" plain round size="small" :disabled="!dataKeyLen" @click="copyDataKey">
              复制
            </el-button>
          </div>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">恢复密钥</div>
            <div class="pref-desc">忘记主密码时凭它重置；服务端只存哈希，重新生成后仅展示一次</div>
          </div>
          <el-button type="primary" plain round @click="onRegenRecovery">重新生成</el-button>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">启动页</div>
            <div class="pref-desc">解锁密钥库后首先打开的页面</div>
          </div>
          <el-select :model-value="startPage" style="width: 130px" @change="onStartPageChange">
            <el-option v-for="o in startPageOptions" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">剪贴板自动清除</div>
            <div class="pref-desc">复制敏感信息后自动清空系统剪贴板，防止泄露</div>
          </div>
          <el-select :model-value="clipClear" style="width: 130px" @change="onClipClearChange">
            <el-option v-for="o in clipClearOptions" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">回收站自动清理</div>
            <div class="pref-desc">删除的条目超过保留期后自动彻底删除，不可恢复</div>
          </div>
          <el-select :model-value="retention" style="width: 130px" @change="onRetentionChange">
            <el-option v-for="o in retentionOptions" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">自动锁定</div>
            <div class="pref-desc">一段时间无操作后自动锁定密钥库，需要重新输入主密码</div>
          </div>
          <el-select :model-value="autoLock" style="width: 130px" @change="onLockChange">
            <el-option v-for="o in lockOptions" :key="o.value" :label="o.label" :value="o.value" />
          </el-select>
        </div>
        <div class="pref-row">
          <div class="pref-text">
            <div class="pref-label">数据备份与恢复</div>
            <div class="pref-desc">导出全部数据到 JSON 备份文件，或从备份文件恢复（需验证主密码）</div>
          </div>
          <div class="backup-btns">
            <el-button round @click="pickImportFile">恢复备份</el-button>
            <el-button type="primary" plain round :loading="exporting" @click="exportBackup">导出备份</el-button>
            <input ref="importInputRef" type="file" accept=".json" style="display: none" @change="onImportFile" />
          </div>
        </div>
      </div>
    </div>

    <!-- 关于 -->
    <div class="card card-pad block about-card">
      <div class="about-brand">
        <img :src="logoUrl" class="about-logo" alt="TermX" />
        <div class="about-brand-text">
          <div class="about-name">
            TermX
            <span class="ver-pill">v0.1.0</span>
          </div>
          <div class="about-tagline">本地优先的 SSH 密钥与连接凭据管理工具</div>
        </div>
        <span class="about-badge">开源版</span>
      </div>

      <div class="about-grid">
        <div class="about-item about-span">
          <div class="about-k">简介</div>
          <div class="about-v about-desc">
            本地优先的 SSH 密钥与连接凭据管理工具，全部数据加密存储于本机，不上传任何服务器。
          </div>
        </div>
        <div class="about-item">
          <div class="about-k">作者</div>
          <div class="about-v">TermX团队</div>
        </div>
        <div class="about-item">
          <div class="about-k">联系方式</div>
          <div class="about-v">官网 termx.cn</div>
        </div>
      </div>

      <div class="about-group-label">交流群组</div>
      <div class="group-cards">
        <div class="group-card">
          <div class="group-icon"><el-icon :size="17"><ChatDotRound /></el-icon></div>
          <div class="group-body">
            <div class="group-name">QQ 交流群</div>
            <div class="group-sub mono">1095707034</div>
          </div>
          <el-tooltip content="复制群号" placement="top">
            <button type="button" class="icon-op" @click="copyTextOf('1095707034', '群号')">
              <el-icon><CopyDocument /></el-icon>
            </button>
          </el-tooltip>
        </div>
        <div class="group-card">
          <div class="group-icon"><el-icon :size="17"><Link /></el-icon></div>
          <div class="group-body">
            <div class="group-name">GitHub</div>
            <div class="group-sub mono">github.com/nncscn/TermX</div>
          </div>
          <el-tooltip content="复制地址" placement="top">
            <button type="button" class="icon-op" @click="copyTextOf('github.com/nncscn/TermX', '仓库地址')">
              <el-icon><CopyDocument /></el-icon>
            </button>
          </el-tooltip>
        </div>
      </div>

      <div class="about-actions">
        <el-button text type="primary" @click="showLicense = true">开源许可证</el-button>
      </div>
    </div>

    <!-- 许可证弹窗 -->
    <el-dialog v-model="showLicense" title="TermX 开源许可证" width="560px">
      <div class="license-brief">
        本软件仅供学习与技术交流。二次开发须获作者书面授权，禁止任何形式的商业使用。作者不对使用后果承担责任。
      </div>
      <pre class="license-pre">TermX 开源许可证（版本 1.0）

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
    </el-dialog>

    <!-- 数据库切换重启遮罩 -->
    <div class="restart-mask" v-if="dbRestarting">
      <div class="restart-box">
        <el-icon class="is-loading" :size="30"><Loading /></el-icon>
        <div class="restart-title">正在重启后端并切换数据库…</div>
        <div class="restart-sub">约 3~10 秒，完成后自动恢复；若切换到全新数据库将跳转登录页</div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.settings-page {
  max-width: 760px;
}

.block {
  margin-bottom: 16px;
}

.block-head {
  display: flex;
  align-items: center;
  gap: 14px;
  margin-bottom: 16px;
}
.block-title {
  font-size: 15px;
  font-weight: 700;
}
.block-desc {
  margin-top: 3px;
  font-size: 12.5px;
  color: var(--ky-text-sub);
}

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}
@media (max-width: 640px) {
  .form-grid {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
.pwd-form :deep(.el-form-item) {
  margin-bottom: 12px;
}
.form-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--ky-border);
}

.strength {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: -4px 0 10px;
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
  margin-bottom: 12px;
  font-size: 12.5px;
  color: #f56c6c;
}

.pref-rows {
  display: flex;
  flex-direction: column;
}
.pref-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  padding: 13px 0;
}
.pref-rows > .pref-row:not(:first-of-type) {
  border-top: 1px solid var(--ky-border);
  padding-top: 14px;
}
.pref-text {
  flex: 1;
  min-width: 220px;
}
.pref-label {
  font-size: 14px;
  font-weight: 600;
}
.pref-desc {
  margin-top: 2px;
  font-size: 12.5px;
  color: var(--ky-text-faint);
}
/* 平滑展开容器（grid 0fr→1fr 高度动画 + 淡入） */
.expand-box {
  display: grid;
  grid-template-rows: 0fr;
  opacity: 0;
  border-top: 1px solid transparent;
  transition:
    grid-template-rows 0.32s var(--ease-out),
    opacity 0.26s ease,
    border-color 0.32s ease;
}
.expand-box.open {
  grid-template-rows: 1fr;
  opacity: 1;
  border-top-color: var(--ky-border);
}
.expand-box:not(.open) {
  pointer-events: none;
}
.expand-inner {
  overflow: hidden;
  min-height: 0;
  visibility: hidden;
  transition: visibility 0s 0.32s;
}
.expand-box.open .expand-inner {
  visibility: visible;
  transition-delay: 0s;
}
.expand-inner .pref-row + .pref-row {
  border-top: 1px solid var(--ky-border);
  padding-top: 14px;
}
/* 两卡统一控件宽度系统 */
.set-input {
  width: 240px;
}
.set-port {
  width: 150px;
}
.conn-preview {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 2px;
  padding: 10px 14px;
  border-radius: 10px;
  background: #f7fafd;
  border: 1px solid #e8eef7;
}
.conn-preview .el-icon {
  color: var(--ky-primary);
  flex-shrink: 0;
}
.conn-preview code {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  letter-spacing: 0.3px;
  color: #47536b;
  word-break: break-all;
}
.db-file {
  padding: 5px 12px;
  border-radius: 8px;
  background: #f5f8fc;
  border: 1px solid #e4ebf5;
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  color: #47536b;
}
.conn-ctrl {
  display: flex;
  align-items: center;
  gap: 10px;
}
.port-free {
  color: #2e8f6d;
  font-size: 12.5px;
}
.port-busy {
  color: #e08c2f;
  font-size: 12.5px;
}
.conn-state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12.5px;
  color: var(--ky-text-faint);
}
.conn-state .dot-off {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #c3ccd9;
}
.conn-state .dot-on {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #3ecf8e;
  box-shadow: 0 0 0 3px rgba(62, 207, 142, 0.18);
}
.backup-btns {
  display: flex;
  align-items: center;
  gap: 8px;
}

.dk-code {
  display: block;
  margin-top: 8px;
  padding: 8px 12px;
  border-radius: 8px;
  background: #f5f8fc;
  border: 1px dashed #b9d3f0;
  font-size: 12.5px;
  word-break: break-all;
  color: #33445c;
  user-select: all;
}

/* 待生效变更提示条 */
.pending-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  margin-bottom: 14px;
  border-radius: 10px;
  background: #fdf6ec;
  border: 1px solid #f5dab6;
  color: #b88230;
  font-size: 12.5px;
  flex-wrap: wrap;
}
.pending-banner b {
  color: #a06a1b;
}
.pending-banner .el-button {
  margin-left: auto;
}

/* 数据库切换重启遮罩 */
.restart-mask {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(15, 22, 36, 0.45);
  backdrop-filter: blur(2px);
}
.restart-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 34px 44px;
  border-radius: 16px;
  background: #fff;
  box-shadow: 0 18px 46px rgba(15, 22, 36, 0.28);
  color: var(--ky-primary);
}
.restart-title {
  font-size: 15px;
  font-weight: 700;
  color: #1f2430;
}
.restart-sub {
  font-size: 12px;
  color: var(--ky-text-faint);
}


.about-card {
  padding: 22px;
}
.about-brand {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 18px 20px;
  border-radius: 12px;
  background: linear-gradient(135deg, #f2f7ff 0%, #e9f1fd 100%);
  border: 1px solid #dde9fa;
}
.about-badge {
  margin-left: auto;
  flex-shrink: 0;
  padding: 4px 12px;
  border-radius: 999px;
  background: #fff;
  border: 1px solid #cfe0f8;
  color: var(--ky-primary);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 1px;
}
.about-logo {
  width: 54px;
  height: 54px;
  border-radius: 14px;
  box-shadow: 0 10px 22px rgba(23, 58, 105, 0.24);
}
.about-name {
  font-size: 17px;
  font-weight: 700;
  line-height: 1.3;
}
.about-tagline {
  margin-top: 4px;
  font-size: 12.5px;
  color: var(--ky-text-faint);
}
.about-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px 24px;
  margin-top: 20px;
  padding: 18px 0;
  border-top: 1px solid var(--ky-border);
  border-bottom: 1px solid var(--ky-border);
}
.about-k {
  font-size: 11.5px;
  letter-spacing: 1px;
  color: #8a94a6;
}
.about-v {
  margin-top: 3px;
  font-size: 13.5px;
  font-weight: 500;
  color: var(--ky-text-main);
}
.about-item.about-span {
  grid-column: 1 / -1;
}
.about-v.about-desc {
  margin-top: 5px;
  font-weight: 400;
  font-size: 13px;
  line-height: 1.7;
  color: var(--ky-text-sub);
}
.about-group-label {
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid var(--ky-border);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 2px;
  color: #8a94a6;
}
.group-cards {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
  margin-top: 12px;
}
.group-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 13px 14px;
  border-radius: 12px;
  background: #fafbfd;
  border: 1px solid #e8ecf4;
  cursor: default;
  transition: all 0.2s var(--ease-out);
}
.group-card:hover {
  border-color: #c9dcf7;
  background: #f5f9ff;
  transform: translateY(-1px);
}
.group-icon {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 10px;
  background: var(--ky-primary-soft);
  color: var(--ky-primary);
}
.group-body {
  flex: 1;
  min-width: 0;
}
.group-name {
  font-size: 13px;
  font-weight: 600;
}
.group-sub {
  margin-top: 2px;
  font-size: 11.5px;
  color: var(--ky-text-faint);
}
.group-sub.mono {
  font-family: 'JetBrains Mono', Consolas, monospace;
  letter-spacing: 0.3px;
}
.about-actions {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 4px;
  margin-top: 16px;
}
@media (max-width: 640px) {
  .about-grid {
    grid-template-columns: 1fr;
  }
  .group-cards {
    grid-template-columns: 1fr;
  }
}
.ver-pill {
  display: inline-block;
  margin-left: 6px;
  padding: 1px 9px;
  border-radius: 999px;
  background: var(--ky-primary-soft);
  color: var(--ky-primary);
  font-size: 11px;
  font-weight: 600;
  font-family: 'JetBrains Mono', Consolas, monospace;
  vertical-align: 2px;
}
.license-brief {
  font-size: 13px;
  line-height: 1.8;
}
.license-pre {
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
</style>
