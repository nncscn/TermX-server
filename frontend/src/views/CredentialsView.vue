<script setup>
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  PROTOCOLS,
  createCredential,
  decryptCredSecret,
  defaultPort,
  listAllGroups,
  listCredentials,
  removeCredential,
  updateCredential
} from '@/api/credentials'
import { getSessionDataKey } from '@/utils/crypto'
import { timeAgo } from '@/utils/format'

const loading = ref(true)
const creds = ref([])
const allGroups = ref([])

const keyword = ref('')
const authFilter = ref('')
const groupFilter = ref('')

// 分页：每页条数自适应可视高度（正好铺满表格区，不留底部空白）
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
    // 二次校正：仍有残余则再压 1px（不低于 2px）
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
const visibleCreds = computed(() =>
  groupFilter.value === '未分组' ? creds.value.filter((c) => !c.group) : creds.value
)
const pagedCreds = computed(() =>
  visibleCreds.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value)
)
const groupOptions = computed(() => {
  const opts = [...allGroups.value]
  if (creds.value.some((c) => !c.group) && !opts.includes('未分组')) {
    opts.push('未分组')
  }
  return opts
})

// 筛选变化回到第一页
watch([keyword, authFilter, groupFilter], () => {
  page.value = 1
})

async function refresh() {
  loading.value = true
  try {
    ;[creds.value, allGroups.value] = await Promise.all([
      listCredentials({
        keyword: keyword.value,
        authType: authFilter.value,
        group: groupFilter.value === '未分组' ? '' : groupFilter.value
      }),
      listAllGroups()
    ])
  } catch (e) {
    creds.value = []
    ElMessage.error(e.message || '加载凭据失败')
  } finally {
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

let searchTimer = null
function onSearch() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(refresh, 300)
}

const protoLabel = (v) => PROTOCOLS.find((p) => p.value === v)?.label || v.toUpperCase()

// ---------- 新增 / 编辑 ----------
const dialogVisible = ref(false)
const editingId = ref(null)

// 关闭瞬间点击穿透到下层编辑图标会导致弹窗立刻重开，短暂忽略
let lastClosedAt = 0
watch(dialogVisible, (v) => {
  if (!v) lastClosedAt = Date.now()
})
const clickGuardOk = () => Date.now() - lastClosedAt > 350
const form = reactive({
  name: '',
  protocol: 'ssh',
  host: '',
  port: 22,
  username: '',
  authType: 'password',
  password: '',
  keyContent: '',
  group: '',
  proxy: { host: '', username: '', password: '' },
  serial: { baudRate: 115200, dataBits: 8, stopBits: 1, parity: 'none' },
  remark: ''
})

// 协议感知：各协议的字段差异
const isSSH = computed(() => form.protocol === 'ssh')
const isSerial = computed(() => form.protocol === 'serial')
const needUser = computed(() => ['ssh', 'rdp', 'telnet'].includes(form.protocol))
const userPlaceholder = computed(() =>
  ({ ssh: 'root', rdp: 'administrator 或 DOMAIN\\user', telnet: 'root' }[form.protocol] || '')
)
const pwdLabel = computed(() =>
  ({ ssh: '登录密码', rdp: '登录密码', telnet: '登录密码', vnc: 'VNC 密码' }[form.protocol] || '密码')
)

/** 切换协议时自动切换默认端口（serial 无端口），非 SSH 协议固定密码认证 */
function onProtocolChange(v) {
  form.port = defaultPort(v)
  if (v !== 'ssh') form.authType = 'password'
}

const portOk = computed(() =>
  form.protocol === 'serial' ? true : form.port >= 1 && form.port <= 65535
)

const formValid = computed(() => {
  if (termxMode.value) return !!form.name.trim()
  return (
    form.name.trim() &&
    form.host.trim() &&
    portOk.value &&
    (needUser.value ? !!form.username.trim() : true) &&
    (isSSH.value
      ? form.authType === 'password'
        ? !!form.password
        : !!form.keyContent.trim()
      : form.protocol === 'serial'
        ? true
        : !!form.password)
  )
})

function blankProxy() {
  return { host: '', username: '', password: '' }
}
const blankSerial = () => ({ baudRate: 115200, dataBits: 8, stopBits: 1, parity: 'none' })

const unlocked = ref(false)
const verifyPwd = ref('')
const verifyErr = ref('')
const verifying = ref(false)
const editingBlob = ref('')
const termxMode = ref(false)

function onFillSessionKey() {
  const k = getSessionDataKey()
  if (k) verifyPwd.value = k
  else verifyErr.value = '本次登录未获取到数据密钥'
}

async function onVerify() {
  if (!verifyPwd.value) {
    verifyErr.value = '请输入数据密钥'
    return
  }
  verifying.value = true
  try {
    const sec = decryptCredSecret(verifyPwd.value, editingBlob.value)
    form.host = sec.host
    form.port = sec.port ?? defaultPort(form.protocol)
    form.username = sec.username
    form.password = sec.password
    form.keyContent = sec.keyContent || ''
    form.proxy = { ...blankProxy(), ...(sec.proxy || {}) }
    form.serial = { ...blankSerial(), ...(sec.serial || {}) }
    unlocked.value = true
    verifyErr.value = ''
  } catch (e) {
    verifyErr.value = e.message || '验证失败'
  } finally {
    verifying.value = false
  }
}

function openCreate() {
  if (!clickGuardOk()) return
  editingId.value = null
  unlocked.value = true
  termxMode.value = false
  verifyPwd.value = getSessionDataKey()
  verifyErr.value = ''
  Object.assign(form, {
    name: '',
    protocol: 'ssh',
    host: '',
    port: 22,
    username: '',
    authType: 'password',
    password: '',
    keyContent: '',
    group: groupFilter.value || '',
    proxy: blankProxy(),
    serial: blankSerial(),
    remark: ''
  })
  dialogVisible.value = true
}

function openEdit(row) {
  if (!clickGuardOk()) return
  editingId.value = row.id
  termxMode.value = !!row.isTermx
  // 基本信息编辑；普通条目仍走"输入数据密钥解锁"两步流
  unlocked.value = !!row.isTermx
  verifyPwd.value = getSessionDataKey()
  verifyErr.value = ''
  editingBlob.value = row.secretBlob || ''
  Object.assign(form, {
    name: row.name,
    protocol: row.protocol || 'ssh',
    host: '', // 密文层：输入数据密钥后本地解密回填
    port: defaultPort(row.protocol || 'ssh'),
    username: '',
    password: '',
    keyContent: '',
    authType: row.authType,
    group: row.group || '',
    proxy: blankProxy(),
    serial: blankSerial(),
    remark: row.remark
  })
  dialogVisible.value = true
}

const saving = ref(false)

async function onSave() {
  if (!formValid.value) {
    ElMessage.warning('请完整填写名称、主机、用户名，并补全认证信息')
    return
  }
  saving.value = true
  try {
    const payload = {
      ...form,
      proxy: {
        host: form.proxy.host.trim(),
        username: form.proxy.username.trim(),
        password: form.proxy.password
      },
      serial: { ...form.serial }
    }
    if (editingId.value) {
      await updateCredential(editingId.value, payload, { metadataOnly: termxMode.value })
      ElMessage.success(termxMode.value ? '基本信息已更新（连接信息由 TermX 客户端加密保留）' : '凭据已更新')
    } else {
      await createCredential(payload)
      ElMessage.success('凭据已创建')
    }
    dialogVisible.value = false
    await refresh()
  } finally {
    saving.value = false
  }
}

// ---------- 删除 ----------
async function onRemove(row) {
  try {
    await ElMessageBox.confirm(
      `确定删除凭据「${row.name}」？删除后可在回收站恢复。`,
      '删除凭据',
      { type: 'info', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  await removeCredential(row.id)
  ElMessage.success('已移入回收站')
  await refresh()
}

// 分组标签配色：按组名哈希取色相（同名恒定、不同名尽量不同色），
// "未分组"用中性灰以示区别
const groupTagStyle = (g) => {
  if (!g) {
    return { backgroundColor: '#f0f2f5', color: '#8a94a6', borderColor: '#e3e7ee' }
  }
  let h = 0
  for (let i = 0; i < g.length; i++) h = (h * 31 + g.charCodeAt(i)) >>> 0
  h %= 360
  return {
    backgroundColor: `hsl(${h}, 85%, 92%)`,
    color: `hsl(${h}, 55%, 30%)`,
    borderColor: `hsl(${h}, 55%, 78%)`
  }
}
</script>

<template>
  <div class="page stagger">
    <!-- 工具栏 -->
    <div class="toolbar">
      <el-input
        v-model="keyword"
        placeholder="搜索名称 / 分组 / 备注"
        clearable
        @input="onSearch"
      >
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>
      <el-select v-model="authFilter" placeholder="全部认证方式" clearable @change="refresh">
        <el-option label="密码认证" value="password" />
        <el-option label="密钥认证" value="key" />
      </el-select>
      <el-select v-model="groupFilter" placeholder="全部分组" clearable @change="refresh">
        <el-option v-for="g in groupOptions" :key="g" :label="g" :value="g" />
      </el-select>
      <div class="spacer"></div>
      <el-button type="primary" round @click="openCreate">
        <el-icon style="margin-right: 5px"><Plus /></el-icon>
        新增凭据
      </el-button>
    </div>

    <div class="card table-card" v-loading="loading">
      <el-table :data="pagedCreds" style="width: 100%">
        <el-table-column label="名称" min-width="240">
          <template #default="{ row }">
            <div class="name-cell">
              <div class="name-line">
                <el-tag
                  :style="groupTagStyle(row.group)"
                  effect="light"
                  size="small"
                  round
                >
                  {{ row.group || '未分组' }}
                </el-tag>
                <span class="name">{{ row.name }}</span>
              </div>
              <div v-if="row.remark" class="remark">{{ row.remark }}</div>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="协议" width="100" align="center">
          <template #default="{ row }">
            <span class="proto">{{ protoLabel(row.protocol) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="认证方式" width="110" align="center">
          <template #default="{ row }">
            <el-tag
              v-if="row.authType === 'key'"
              type="primary"
              effect="light"
              size="small"
              round
            >
              密钥
            </el-tag>
            <el-tag v-else type="info" effect="light" size="small" round>密码</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最后连接" width="115">
          <template #default="{ row }">
            <span class="muted">{{ timeAgo(row.lastUsedAt) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="110" align="right">
          <template #default="{ row }">
            <el-tooltip content="编辑" placement="top">
              <button type="button" class="icon-op" @click="openEdit(row)">
                <el-icon><EditPen /></el-icon>
              </button>
            </el-tooltip>
            <span class="op-divider"></span>
            <el-tooltip content="删除" placement="top">
              <button type="button" class="icon-op danger" @click="onRemove(row)">
                <el-icon><Delete /></el-icon>
              </button>
            </el-tooltip>
          </template>
        </el-table-column>
        <template #empty>
          <div class="ky-empty">
            <div class="empty-icon"><el-icon :size="26"><Connection /></el-icon></div>
            <div class="empty-title">没有匹配的凭据</div>
            <div class="empty-desc">调整筛选条件，或点击右上角「新增凭据」添加服务器</div>
          </div>
        </template>
      </el-table>
      <div class="pager-row">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="visibleCreds.length"
          :page-sizes="pageSizeOptions"
          layout="total, sizes, prev, pager, next"
          background
        />
      </div>
    </div>

    <!-- 新增 / 编辑弹窗 -->
    <el-dialog
      v-model="dialogVisible"
      width="640px"
      class="cred-dialog"
    >
      <template #header>
        <div class="dlg-head">
          <span class="dlg-title">{{ editingId ? (unlocked ? '编辑凭据' : '输入数据密钥') : '新增连接凭据' }}</span>
          <span v-if="editingId && unlocked" class="unlocked-pill">
            <el-icon :size="12"><CircleCheckFilled /></el-icon>
            已解锁 · 仅本机
          </span>
          <span v-else-if="editingId" class="locked-pill">
            <el-icon :size="12"><Lock /></el-icon>
            密文已锁定
          </span>
        </div>
      </template>

      <!-- 第一步：验证主密码 -->
      <div v-if="editingId && !unlocked" class="verify-pane">
        <div class="verify-icon"><el-icon :size="26"><Lock /></el-icon></div>
        <div class="verify-title">输入数据密钥解锁</div>
        <div class="verify-desc">「{{ form.name }}」的连接信息为端到端加密，输入数据密钥（Base64 43 字符）在本机解密后才能编辑，服务端不参与解密</div>
        <div class="verify-input">
          <el-input
            v-model="verifyPwd"
            type="password"
            show-password
            size="large"
            placeholder="数据密钥（Base64 43 字符）"
            @keyup.enter="onVerify"
          />
        </div>
        <button type="button" class="verify-cancel" style="margin-top: 8px" @click="onFillSessionKey">用本次登录的密钥填入</button>
        <div class="verify-err">{{ verifyErr || ' ' }}</div>
        <el-button
          type="primary"
          round
          size="large"
          class="verify-btn"
          :loading="verifying"
          @click="onVerify"
        >
          解锁并编辑
        </el-button>
        <button type="button" class="verify-cancel" @click="dialogVisible = false">取消</button>
        <div class="verify-foot">解密仅在本机完成，数据密钥不会离开这台设备</div>
      </div>

      <!-- 第二步：编辑表单 -->
      <el-form v-else label-position="top" class="cred-form">
        <div class="sec-title">基本信息</div>
        <div class="form-grid">
          <el-form-item label="名称" required>
            <el-input v-model="form.name" placeholder="例如：生产 Web 服务器" maxlength="40" />
          </el-form-item>
          <el-form-item label="分组">
            <el-select
              v-model="form.group"
              filterable
              allow-create
              default-first-option
              clearable
              placeholder="选择或输入新分组"
            >
              <el-option label="未分组" value="" />
              <el-option v-for="g in allGroups" :key="g" :label="g" :value="g" />
            </el-select>
          </el-form-item>
        </div>
        <div class="form-grid">
          <el-form-item label="协议">
            <el-select v-model="form.protocol" :disabled="!!editingId" @change="onProtocolChange">
              <el-option v-for="p in PROTOCOLS" :key="p.value" :label="p.label" :value="p.value" />
            </el-select>
            <div v-if="editingId" class="field-hint">协议在创建时确定，不可更改</div>
          </el-form-item>
          <el-form-item label="会话 ID">
            <el-input :model-value="editingId || '保存后自动生成'" readonly class="client-id" />
          </el-form-item>
        </div>

        <el-alert
          v-if="termxMode"
          type="info"
          :closable="false"
          show-icon
          title="此条目由 TermX 桌面客户端加密同步"
          description="连接信息（主机/账号/密码）为客户端端到端加密，Web 端无法解密与编辑；此处可修改名称、分组、备注，保存后 TermX 客户端下次同步自动生效。"
          style="margin: 10px 0"
        />

        <template v-if="!termxMode">
        <div class="sec-title">
          连接信息 · {{ protoLabel(form.protocol) }}
          <span class="sec-hint"><el-icon :size="12"><Lock /></el-icon>密文存储 · 服务端不可见</span>
        </div>

        <!-- 串口：设备路径 + 串口参数，无用户名/认证 -->
        <template v-if="isSerial">
          <el-form-item label="设备路径" required>
            <el-input v-model="form.host" placeholder="/dev/ttyUSB0 或 COM3" />
          </el-form-item>
          <div class="form-grid quad">
            <el-form-item label="波特率">
              <el-select v-model="form.serial.baudRate">
                <el-option v-for="b in [9600, 19200, 38400, 57600, 115200]" :key="b" :label="`${b}`" :value="b" />
              </el-select>
            </el-form-item>
            <el-form-item label="数据位">
              <el-select v-model="form.serial.dataBits">
                <el-option :value="8" label="8" />
                <el-option :value="7" label="7" />
              </el-select>
            </el-form-item>
            <el-form-item label="停止位">
              <el-select v-model="form.serial.stopBits">
                <el-option :value="1" label="1" />
                <el-option :value="2" label="2" />
              </el-select>
            </el-form-item>
            <el-form-item label="校验位">
              <el-select v-model="form.serial.parity">
                <el-option value="none" label="无" />
                <el-option value="odd" label="奇校验" />
                <el-option value="even" label="偶校验" />
              </el-select>
            </el-form-item>
          </div>
        </template>

        <!-- SSH / RDP / Telnet / VNC：主机 + 端口 -->
        <template v-else>
          <div class="form-grid">
            <el-form-item label="主机地址" required>
              <el-input v-model="form.host" placeholder="192.168.1.10 或 host.example.com" />
            </el-form-item>
            <el-form-item label="端口" required>
              <el-input-number
                v-model="form.port"
                :min="1"
                :max="65535"
                controls-position="right"
              />
            </el-form-item>
          </div>

          <!-- SSH：用户名 + 认证方式（密码/密钥），RDP/Telnet：用户名 + 密码 -->
          <div v-if="needUser" class="form-grid">
            <el-form-item label="用户名" required>
              <el-input v-model="form.username" :placeholder="userPlaceholder" />
            </el-form-item>
            <el-form-item v-if="isSSH" label="认证方式">
              <el-radio-group v-model="form.authType">
                <el-radio-button value="password">密码</el-radio-button>
                <el-radio-button value="key">SSH 密钥</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <el-form-item v-else :label="pwdLabel" required>
              <el-input
                v-model="form.password"
                type="password"
                show-password
                placeholder="登录密码，仅保存在本机"
              />
            </el-form-item>
          </div>

          <el-form-item v-if="isSSH && form.authType === 'password'" :label="pwdLabel" required>
            <el-input
              v-model="form.password"
              type="password"
              show-password
              placeholder="登录密码，仅保存在本机"
            />
          </el-form-item>
          <el-form-item v-else-if="isSSH" label="密钥内容" required>
            <el-input
              v-model="form.keyContent"
              type="textarea"
              :autosize="{ minRows: 3, maxRows: 8 }"
              placeholder="粘贴私钥/公钥字符串（BEGIN OPENSSH PRIVATE KEY… 或 ssh-ed25519 AAAA…），密文存储"
            />
          </el-form-item>
          <el-form-item v-if="isSSH && form.authType === 'key'" label="解密口令（可选）">
            <el-input
              v-model="form.password"
              type="password"
              show-password
              placeholder="私钥的 passphrase（如有）"
            />
          </el-form-item>
          <el-form-item v-else-if="form.protocol === 'vnc'" :label="pwdLabel" required>
            <el-input
              v-model="form.password"
              type="password"
              show-password
              placeholder="VNC 连接密码，仅保存在本机"
            />
          </el-form-item>
        </template>

        <div class="sec-title">
          代理
          <span class="sec-hint plain">可选 · 密文存储</span>
        </div>
        <div class="form-grid">
          <el-form-item label="代理地址">
            <el-input v-model="form.proxy.host" placeholder="proxy.corp.cn:1080，留空不启用" />
          </el-form-item>
          <el-form-item label="代理用户名">
            <el-input v-model="form.proxy.username" placeholder="proxy-user" />
          </el-form-item>
        </div>
        <el-form-item label="代理密码">
          <el-input
            v-model="form.proxy.password"
            type="password"
            show-password
            placeholder="代理密码，仅保存在本机"
          />
        </el-form-item>
        </template>

        <div class="sec-title">
          备注
          <span class="sec-hint plain">可选 · 与 TermX 客户端同步</span>
        </div>
        <el-form-item>
          <el-input
            v-model="form.remark"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="用途说明，例如：主站 + API 网关"
            maxlength="120"
            show-word-limit
          />
        </el-form-item>
      </el-form>
      <template v-if="!editingId || unlocked" #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" round :loading="saving" :disabled="!formValid" @click="onSave">
          {{ editingId ? '保存修改' : '创建凭据' }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.name-cell {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 3px;
  padding: 9px 0;
}
.name {
  font-weight: 600;
}
.name-line {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.remark {
  font-size: 12px;
  color: var(--ky-text-faint);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 360px;
}
.proto {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.5px;
  color: #47536b;
}
.muted {
  color: var(--ky-text-faint);
  font-size: 12.5px;
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

.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 16px;
}
.form-grid.quad {
  grid-template-columns: repeat(4, 1fr);
  gap: 0 12px;
}
@media (max-width: 640px) {
  .form-grid {
    grid-template-columns: 1fr;
    gap: 0;
  }
  .form-grid.quad {
    grid-template-columns: 1fr 1fr;
  }
}
.field-hint {
  margin-top: 5px;
  font-size: 12px;
  color: var(--ky-text-faint);
  line-height: 1.5;
}

/* 弹窗头部 */
.dlg-head {
  display: flex;
  align-items: center;
  gap: 10px;
}
.dlg-title {
  font-size: 16px;
  font-weight: 700;
  color: #1f2430;
}
.unlocked-pill,
.locked-pill {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 500;
}
.unlocked-pill {
  background: #eef9f4;
  border: 1px solid #d3f0e4;
  color: #2e8f6d;
}
.locked-pill {
  background: #f2f5fa;
  border: 1px solid #e0e6f0;
  color: #8a94a6;
}

/* 验证面板 */
.verify-pane {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 30px 40px 22px;
}
.verify-icon {
  width: 64px;
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: #eef4fd;
  color: var(--ky-primary);
  box-shadow: 0 10px 22px rgba(30, 111, 224, 0.16);
}
.verify-title {
  margin-top: 16px;
  font-size: 16px;
  font-weight: 700;
}
.verify-desc {
  margin-top: 7px;
  max-width: 380px;
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--ky-text-faint);
}
.verify-input {
  width: 320px;
  margin-top: 20px;
}
.verify-err {
  height: 22px;
  margin-top: 6px;
  font-size: 12px;
  color: #e05252;
}
.verify-btn {
  width: 320px;
  margin-top: 2px;
}
.verify-cancel {
  margin-top: 10px;
  border: none;
  background: transparent;
  font-size: 12.5px;
  color: var(--ky-text-faint);
  cursor: pointer;
}
.verify-cancel:hover {
  color: var(--ky-text-sub);
}
.verify-foot {
  margin-top: 18px;
  font-size: 11.5px;
  color: #b3bccb;
}

.sec-title {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 20px 0 2px;
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 1.5px;
  color: #8a94a6;
}
.sec-title:first-child {
  margin-top: 2px;
}
.sec-title::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--ky-border);
}
.sec-hint {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11.5px;
  font-weight: 400;
  letter-spacing: 0;
  color: var(--ky-primary);
  background: var(--ky-primary-soft);
  padding: 2px 9px;
  border-radius: 999px;
}
.sec-hint.plain {
  color: var(--ky-text-faint);
  background: #f2f5fa;
}
.client-id :deep(.el-input__wrapper) {
  background: #f7f9fc;
  box-shadow: none;
  border: 1px dashed #d4dcea;
}
.client-id :deep(input) {
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 12px;
  color: var(--ky-text-faint);
}
.cred-form :deep(.el-form-item) {
  margin-bottom: 14px;
}
.cred-form :deep(.el-select) {
  width: 100%;
}
.cred-form :deep(.el-input-number) {
  width: 100%;
}
</style>

<style>
/* 弹窗本体（el-dialog 传送到 body，需非 scoped 样式） */
.cred-dialog {
  border-radius: 16px;
  overflow: hidden;
  margin-top: 6vh;
}
.cred-dialog .el-dialog__header {
  margin: 0;
  padding: 18px 24px 14px;
  border-bottom: 1px solid var(--ky-border, #e5e8f2);
}
.cred-dialog .el-dialog__title {
  font-size: 16px;
  font-weight: 700;
  color: #1f2430;
}
.cred-dialog .el-dialog__body {
  padding: 6px 24px 8px;
  max-height: calc(100vh - 230px);
  overflow-y: auto;
}
.cred-dialog .el-dialog__footer {
  padding: 14px 24px 18px;
  border-top: 1px solid var(--ky-border, #e5e8f2);
}
</style>
