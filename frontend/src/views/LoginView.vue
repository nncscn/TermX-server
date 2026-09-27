<script setup>
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import logoUrl from '@/assets/logo.png'
import {
  clearLoginFails,
  getLoginFails,
  getRememberedName,
  recordLoginFail,
  setRememberedName
} from '@/api/local'
import {
  apiForgotReset,
  apiForgotVerify,
  apiGetCaptcha,
  apiLogin
} from '@/api/backend'
import { apiGetSettings } from '@/api/settings'
import { setSessionDataKey } from '@/utils/crypto'
import { copyText } from '@/utils/format'

const route = useRoute()
const router = useRouter()

const showForgot = ref(route.query.flip === '1')
const remembered = getRememberedName()
const accountName = ref(
  typeof route.query.account === 'string' && route.query.account ? route.query.account : remembered
)

// 自动锁定 / 会话过期跳转过来时给出一行说明
if (route.query.locked === '1') {
  ElMessage.info('因长时间未操作，密钥库已自动锁定，请重新输入主密码')
} else if (route.query.expired === '1') {
  ElMessage.info('登录已过期，请重新登录')
}

function goForgot() {
  recoveryKeyInput.value = ''
  keyError.value = ''
  keyVerified.value = false
  showForgot.value = true
  router.replace({ query: { ...route.query, flip: '1' } })
}

function backToLogin() {
  keyVerified.value = false
  showForgot.value = false
  router.replace({ query: { ...route.query, flip: undefined } })
}

const password = ref('')
const showPwd = ref(false)
const loading = ref(false)
const error = ref('')
const shakeTick = ref(0)

const rememberMe = ref(!!remembered)

const formValid = computed(() => !!accountName.value.trim() && !!password.value)

async function onSubmit() {
  if (loading.value) return
  const name = accountName.value.trim()
  if (!name) {
    error.value = '请输入账户名'
    shakeTick.value++
    return
  }
  if (!password.value) {
    error.value = '请输入主密码'
    shakeTick.value++
    return
  }
  // 冷却中直接拦截（按钮本身已呈倒计时禁用态）
  const st = getLoginFails(name)
  if (st.cooldownUntil > Date.now()) {
    startCooldownTick(st.cooldownUntil)
    ElMessage.warning('失败次数过多，请等待冷却结束')
    return
  }
  loading.value = true
  error.value = ''
  try {
    if (st.needCaptcha) {
      openCaptcha()
      return
    }
    await succeedLogin()
  } finally {
    loading.value = false
  }
}

/** 尝试登录：返回 { ok } 或 { kind: 'captcha' | 'captcha-wrong' | 'auth', msg } */
async function tryLogin(captcha) {
  const name = accountName.value.trim()
  try {
    const data = await apiLogin(name, password.value, rememberMe.value, captcha)
    return { ok: true, data }
  } catch (e) {
    if (e.message === '请完成安全验证') return { kind: 'captcha', msg: e.message }
    if (e.message === '验证码不正确，请重试') return { kind: 'captcha-wrong', msg: e.message }
    return { kind: 'auth', msg: e.message }
  }
}

/** 登录成功后的统一收尾（迁移完成时先一次性展示新恢复密钥） */
async function finishLogin(loginData) {
  const name = accountName.value.trim()
  clearLoginFails(name)
  setRememberedName(rememberMe.value ? name : null)
  // 数据密钥：本次会话的本地加解密密钥（内存缓存 + 一次性展示便于抄写）
  if (loginData && loginData.data_key) {
    setSessionDataKey(loginData.data_key)
    try {
      ElMessageBox.alert(
        `<div style="font-family:monospace;font-size:13px;letter-spacing:1px;word-break:break-all;padding:12px;border:1px dashed #b9d3f0;border-radius:8px;background:#f5f8fc;margin:10px 0">${loginData.data_key}</div><div style="font-size:12px;color:#5e6b81">本次会话的数据密钥（编辑弹窗解密用，已自动填入），请抄写保存以便手动输入</div>`,
        '本次会话的数据密钥',
        { dangerouslyUseHTMLString: true, confirmButtonText: '知道了' }
      ).catch(() => {})
    } catch (e) {
      console.warn('数据密钥弹窗失败', e)
    }
  }
  // 旧数据迁移：服务端随登录返回一次性新恢复密钥（旧恢复密钥已作废）
  if (loginData && loginData.recovery_key) {
    try {
      await ElMessageBox.alert(
        `<div style="font-family:monospace;font-size:14px;letter-spacing:1px;word-break:break-all;padding:12px;border:1px dashed #b9d3f0;border-radius:8px;background:#f5f8fc;margin:10px 0">${loginData.recovery_key}</div><div style="font-size:12px;color:#e08c2f">数据已升级为新的加密结构，旧恢复密钥已作废。新恢复密钥仅此一次展示，请立即抄写或复制保存</div>`,
        '重要：新恢复密钥',
        { dangerouslyUseHTMLString: true, confirmButtonText: '我已保存' }
      )
    } catch {
      /* 用户关闭也继续登录 */
    }
  }
  try {
    await apiGetSettings()
  } catch {
    /* 离线或设置读取失败时，用本地缓存兜底 */
  }
  ElMessage.success('密钥库已解锁')
  const startPage = localStorage.getItem('ky_start_page') || '/dashboard'
  router.push(startPage === '/keys' ? '/dashboard' : startPage)
}

async function succeedLogin(captcha) {
  const r = await tryLogin(captcha)
  if (r.ok) {
    await finishLogin(r.data)
    return
  }
  if (r.kind === 'captcha') {
    openCaptcha() // 服务端要求验证码：拉起弹窗
    return
  }
  onAuthFail(r.msg)
}

/** 统一失败处理：不区分账户/密码错误，防止账户枚举；仅轻提示 */
function onAuthFail(serverMessage) {
  const name = accountName.value.trim()
  const st = recordLoginFail(name)
  if (st.cooldownUntil) {
    captchaDialog.value = false
    startCooldownTick(st.cooldownUntil)
    error.value = '失败次数过多，请稍后再试'
    shakeTick.value++
    return
  }
  error.value = serverMessage || '账户或密码错误'
  shakeTick.value++
}

const cooldownRemain = ref(0)
let cooldownTimer = null

function startCooldownTick(until) {
  if (cooldownTimer) clearInterval(cooldownTimer)
  const tick = () => {
    cooldownRemain.value = Math.max(0, Math.ceil((until - Date.now()) / 1000))
    if (cooldownRemain.value <= 0 && cooldownTimer) {
      clearInterval(cooldownTimer)
      cooldownTimer = null
    }
  }
  tick()
  cooldownTimer = setInterval(tick, 1000)
}

function syncCooldown() {
  const name = accountName.value.trim()
  const st = name ? getLoginFails(name) : { cooldownUntil: 0 }
  const until = st.cooldownUntil || 0
  if (until > Date.now()) {
    startCooldownTick(until)
  } else if (!cooldownTimer) {
    cooldownRemain.value = 0
  }
}

watch(accountName, syncCooldown)
onMounted(syncCooldown)
onUnmounted(() => {
  if (cooldownTimer) clearInterval(cooldownTimer)
})

const inCooldown = computed(() => cooldownRemain.value > 0)
const cooldownText = computed(() => {
  const m = Math.floor(cooldownRemain.value / 60)
  const s = cooldownRemain.value % 60
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
})

const captchaDialog = ref(false)
const capLoading = ref(false)
const cap = ref({ captchaId: '', image: '', prompt: '' })
const capClicks = ref([]) // 点击点（原图坐标）+ 序号
const capVerifying = ref(false) // 选满自动提交后的验证中状态
const capError = ref('')
const capExpireRemain = ref(0)
let capExpireTimer = null

const CAPTCHA_NEED = 4 // 需要按成语顺序点满 4 个字
const CAPTCHA_TTL = 60

async function newCaptcha() {
  capLoading.value = true
  capError.value = ''
  capClicks.value = []
  try {
    const data = await apiGetCaptcha()
    cap.value = { captchaId: data.captcha_id, image: data.image, prompt: data.prompt }
    capExpireRemain.value = data.ttl || CAPTCHA_TTL
  } catch (e) {
    capError.value = e.message
    ElMessage.error(e.message)
  } finally {
    capLoading.value = false
  }
}

function startExpireTick() {
  if (capExpireTimer) clearInterval(capExpireTimer)
  capExpireTimer = setInterval(() => {
    capExpireRemain.value -= 1
    if (capExpireRemain.value <= 0) newCaptcha()
  }, 1000)
}

async function openCaptcha() {
  captchaDialog.value = true
  await newCaptcha()
  startExpireTick()
}

function closeCaptcha() {
  captchaDialog.value = false
  capVerifying.value = false
  if (capExpireTimer) {
    clearInterval(capExpireTimer)
    capExpireTimer = null
  }
}

/** 点击序号标记：取消该次选择（后续序号自动前移） */
function removeCapClick(i) {
  if (capVerifying.value) return
  capClicks.value.splice(i, 1)
}

/** 点击图面：换算为原图坐标（300×120）并登记序号 */
function onCaptchaClick(e) {
  if (capClicks.value.length >= CAPTCHA_NEED || capVerifying.value) {
    return
  }
  const wrap = e.currentTarget
  const rect = wrap.getBoundingClientRect()
  const scaleX = 300 / rect.width
  const scaleY = 120 / rect.height
  capClicks.value.push({
    x: +((e.clientX - rect.left) * scaleX).toFixed(1),
    y: +((e.clientY - rect.top) * scaleY).toFixed(1),
    px: +(((e.clientX - rect.left) / rect.width) * 100).toFixed(2),
    py: +(((e.clientY - rect.top) / rect.height) * 100).toFixed(2)
  })
  if (capClicks.value.length === CAPTCHA_NEED) {
    setTimeout(() => {
      const okLen = capClicks.value.length === CAPTCHA_NEED
      const okDlg = captchaDialog.value
      const okVer = !capVerifying.value
      if (okLen && okDlg && okVer) {
        onCaptchaOk()
      }
    }, 400)
  }
}

async function onCaptchaOk() {
  if (capClicks.value.length < CAPTCHA_NEED || loading.value || capVerifying.value) {
    return
  }
  loading.value = true
  capVerifying.value = true
  capError.value = ''
  const payload = {
    captchaId: cap.value.captchaId,
    clicks: capClicks.value.map((c) => ({ x: c.x, y: c.y }))
  }
  try {
    const r = await tryLogin(payload)
    if (r.ok) {
      captchaDialog.value = false
      if (capExpireTimer) {
        clearInterval(capExpireTimer)
        capExpireTimer = null
      }
      await finishLogin(r.data)
      return
    }
    if (r.kind === 'captcha' || r.kind === 'captcha-wrong') {
      capError.value = r.msg
      ElMessage.error(r.msg)
      await new Promise((resolve) => setTimeout(resolve, 500))
      await newCaptcha()
      capError.value = r.msg
      return
    }
    // 验证码通过但账户/密码错误：关弹窗回登录面提示
    captchaDialog.value = false
    if (capExpireTimer) {
      clearInterval(capExpireTimer)
      capExpireTimer = null
    }
    onAuthFail(r.msg)
  } finally {
    loading.value = false
    capVerifying.value = false
  }
}


const recoveryPanel = ref(null)

async function onCopyRecovery() {
  const ok = await copyText(recoveryPanel.value.key)
  ok ? ElMessage.success('恢复密钥已复制') : ElMessage.error('复制失败')
}

function onRecoverySaved() {
  const startPage = localStorage.getItem('ky_start_page') || '/dashboard'
  router.push(startPage === '/keys' ? '/dashboard' : startPage)
}

const recoveryKeyInput = ref('')
const verifying = ref(false)
const keyError = ref('')
const keyShake = ref(0)
const keyVerified = ref(false)
const found = ref(null)

const keyValid = computed(() => !!recoveryKeyInput.value.trim())

async function onVerifyKey() {
  if (verifying.value) return
  const name = accountName.value.trim()
  if (!name) {
    keyError.value = '请先在正面输入账户名，再返回此页找回'
    keyShake.value++
    return
  }
  verifying.value = true
  keyError.value = ''
  try {
    const data = await apiForgotVerify(name, recoveryKeyInput.value.trim())
    found.value = { resetTicket: data.reset_ticket, accountName: name }
    newPassword.value = ''
    newConfirm.value = ''
    // 密钥正确 → 第二次翻面
    keyVerified.value = true
  } catch (e) {
    keyError.value = e.message
    keyShake.value++
  } finally {
    verifying.value = false
  }
}

const newPassword = ref('')
const newConfirm = ref('')
const savingNew = ref(false)
const resetError = ref('')

const newMismatch = computed(
  () => newConfirm.value.length > 0 && newPassword.value !== newConfirm.value
)

const newStrengthLevel = computed(() => {
  const p = newPassword.value
  if (!p) return 0
  let score = 1
  if (p.length >= 8) score++
  if (p.length >= 12) score++
  if (/[a-z]/.test(p) && /[A-Z]/.test(p)) score++
  if (/\d/.test(p) && /[^a-zA-Z0-9]/.test(p)) score++
  return Math.min(score, 4)
})
const newStrengthText = computed(() => ['未设置', '弱', '一般', '较强', '强'][newStrengthLevel.value])

const newCredentialsValid = computed(
  () => newPassword.value.length >= 8 && !newMismatch.value
)

async function onSaveNewCredentials() {
  if (!newCredentialsValid.value || savingNew.value || !found.value) return
  savingNew.value = true
  resetError.value = ''
  try {
    await apiForgotReset(found.value.resetTicket, recoveryKeyInput.value.trim(), newPassword.value)
    ElMessage.success('主密码已重置，请用新密码登录')
    password.value = ''
    backToLogin()
  } catch (e) {
    resetError.value = e.message
  } finally {
    savingNew.value = false
  }
}

</script>

<template>
  <div class="login-page">
    <div class="glow glow-a"></div>
    <div class="glow glow-b"></div>

    <div class="flip-scene">
      <div class="flip3d" :class="{ flipped: showForgot }">
        <!-- ===== 正面 ===== -->
        <div class="face card-face">
            <!-- 登录表单（key 重建仅用于重触发抖动，不再带翻转过渡） -->
            <div
              v-if="!recoveryPanel"
              :key="'form-' + shakeTick"
              class="flip-pane pane-login"
              :class="{ 'ky-shake': error }"
            >
              <img :src="logoUrl" class="logo" alt="TermX" />
              <h1>TermX</h1>
              <p class="sub">自托管凭据管理服务 · 登录</p>

              <div class="field">
                <div class="field-label">账户</div>
                <el-input
                  v-model="accountName"
                  size="large"
                  placeholder="输入账户名"
                  clearable
                >
                  <template #prefix><el-icon><User /></el-icon></template>
                </el-input>
              </div>

              <div class="field">
                <div class="field-label">主密码</div>
                <el-input
                  v-model="password"
                  :type="showPwd ? 'text' : 'password'"
                  size="large"
                  placeholder="主密码"
                  :class="{ 'is-error': error }"
                  @keyup.enter="onSubmit"
                >
                  <template #prefix><el-icon><Lock /></el-icon></template>
                  <template #suffix>
                    <el-icon style="cursor: pointer" @click="showPwd = !showPwd">
                      <View v-if="showPwd" />
                      <Hide v-else />
                    </el-icon>
                  </template>
                </el-input>
              </div>

              <div class="error-line" :style="{ visibility: error ? 'visible' : 'hidden' }">
                <el-icon><CircleCloseFilled /></el-icon>
                {{ error || '占位' }}
              </div>

              <div class="options-row">
                <el-checkbox v-model="rememberMe" size="small">记住我</el-checkbox>
                <el-link type="primary" underline="never" class="forgot-link" @click="goForgot">
                  忘记密码？
                </el-link>
              </div>

              <el-button
                type="primary"
                size="large"
                class="submit"
                :class="{ cooling: inCooldown }"
                :loading="loading"
                :disabled="!formValid || inCooldown"
                round
                @click="onSubmit"
              >
                <el-icon v-if="inCooldown" style="margin-right: 6px"><Clock /></el-icon>
                {{ inCooldown ? `冷却中 ${cooldownText}` : '解 锁' }}
              </el-button>

            </div>

            <!-- 恢复密钥面板 -->
            <div v-else key="recovery" class="flip-pane">
              <div class="recovery-icon">
                <el-icon :size="34" color="#fff"><Key /></el-icon>
              </div>
              <h1>账户已创建</h1>
              <p class="sub">
                「{{ recoveryPanel.account }}」的<b>恢复密钥</b>如下，忘记主密码时凭它找回。
                <b class="warn-text">仅此一次展示，请抄写并妥善保存。</b>
              </p>

              <div class="recovery-key">{{ recoveryPanel.key }}</div>

              <div class="actions">
                <el-button size="large" round @click="onCopyRecovery">
                  <el-icon style="margin-right: 5px"><CopyDocument /></el-icon>
                  复制密钥
                </el-button>
                <el-button type="primary" size="large" round @click="onRecoverySaved">
                  我已保存，进入
                </el-button>
              </div>

              <div class="tips">
                <el-icon><InfoFilled /></el-icon>
                <span>恢复密钥丢失将无法找回主密码，等同于账户数据丢失</span>
              </div>
            </div>
        </div>

        <div class="face card-face face-back">
          <div class="flip-inner" :class="{ flipped: keyVerified }">
            <!-- 背面第一步：只输恢复密钥 -->
            <div
              class="inner-face pane-key"
              :key="'key-' + keyShake"
              :class="{ 'ky-shake': keyError }"
            >
              <div class="key-hero">
                <el-icon :size="30" color="#fff"><Key /></el-icon>
              </div>
              <h1>找回账户</h1>
              <p class="sub">密码忘了？输入恢复密钥即可重置主密码</p>

              <div class="field">
                <div class="field-label">恢复密钥</div>
                <el-input
                  v-model="recoveryKeyInput"
                  type="textarea"
                  :rows="4"
                  resize="none"
                  placeholder="粘贴恢复密钥（XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX）"
                  class="key-input"
                  @keyup.enter="onVerifyKey"
                />
                <div class="key-hint" :class="{ err: keyError }">
                  <template v-if="!keyError">
                    共 8 组字符；忘记主密码时凭它重置，可在「设置 → 数据与偏好」中重新生成
                  </template>
                  <template v-else>⛔ {{ keyError }}</template>
                </div>
              </div>

              <el-button
                type="primary"
                size="large"
                class="submit"
                :loading="verifying"
                :disabled="!keyValid"
                round
                @click="onVerifyKey"
              >
                {{ verifying ? '正在验证…' : '验证密钥' }}
              </el-button>

              <div class="switch-line" @click="backToLogin">
                <el-icon><ArrowLeft /></el-icon>
                返回登录
              </div>
            </div>

            <!-- 背面第二步：设置新账户名与新主密码 -->
            <div class="inner-face inner-backface">
              <div class="ok-icon">
                <el-icon :size="34" color="var(--ky-accent)"><CircleCheckFilled /></el-icon>
              </div>
              <h1>重置主密码</h1>
              <p class="sub">
                恢复密钥验证通过，将为账户「{{ found?.accountName }}」设置新的主密码。
                保存后所有已登录会话将失效：
              </p>

              <div class="field">
                <div class="field-label">新主密码</div>
                <el-input
                  v-model="newPassword"
                  type="password"
                  show-password
                  size="large"
                  placeholder="至少 8 位"
                >
                  <template #prefix><el-icon><Lock /></el-icon></template>
                </el-input>
              </div>

              <div class="field">
                <div class="field-label">确认新主密码</div>
                <el-input
                  v-model="newConfirm"
                  type="password"
                  show-password
                  size="large"
                  placeholder="再次输入新主密码"
                  @keyup.enter="onSaveNewCredentials"
                >
                  <template #prefix><el-icon><Lock /></el-icon></template>
                </el-input>
                <div class="strength">
                  <div class="strength-bars">
                    <span
                      v-for="i in 4"
                      :key="i"
                      :class="{ on: i <= newStrengthLevel }"
                      :data-lv="newStrengthLevel"
                    ></span>
                  </div>
                  <span class="strength-text" :data-lv="newStrengthLevel">
                    强度：{{ newStrengthText }}
                  </span>
                </div>
                <div class="mismatch" :style="{ visibility: newMismatch ? 'visible' : 'hidden' }">
                  <el-icon><CircleCloseFilled /></el-icon>
                  两次输入的密码不一致
                </div>
              </div>

              <div class="error-line" :style="{ visibility: resetError ? 'visible' : 'hidden' }">
                <el-icon><CircleCloseFilled /></el-icon>
                {{ resetError || '占位' }}
              </div>

              <el-button
                type="primary"
                size="large"
                class="submit"
                :loading="savingNew"
                :disabled="!newCredentialsValid"
                round
                @click="onSaveNewCredentials"
              >
                {{ savingNew ? '正在保存…' : '保存并翻回登录' }}
              </el-button>

              <div class="switch-line" @click="keyVerified = false">
                <el-icon><ArrowLeft /></el-icon>
                返回上一步
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 汉字点选验证码弹窗 -->
    <el-dialog
      v-model="captchaDialog"
      width="400px"
      align-center
      class="ky-dialog"
      @close="closeCaptcha"
    >
      <div class="dlg-body">
        <div class="dlg-title">安全验证</div>
        <div class="dlg-desc">{{ cap.prompt || '正在加载验证码…' }}</div>

        <div
          v-loading="capLoading || capVerifying"
          :element-loading-text="capVerifying ? '正在验证…' : '正在加载验证码…'"
          class="cap-click-wrap"
          :class="{ verifying: capVerifying }"
          @click="onCaptchaClick($event)"
        >
          <img v-if="cap.image" :src="cap.image" class="cap-click-img" alt="验证码" draggable="false" />
          <span
            v-for="(c, i) in capClicks"
            :key="i"
            class="cap-mark"
            :style="{ left: c.px + '%', top: c.py + '%' }"
            title="点击取消该选择"
            @click.stop="removeCapClick(i)"
          >{{ i + 1 }}</span>
          <transition name="fade">
            <div v-if="capClicks.length >= CAPTCHA_NEED && !capVerifying" class="cap-ready">
              <el-icon><CircleCheckFilled /></el-icon>
              已选满，正在验证…
            </div>
          </transition>
        </div>

        <div class="cap-toolbar">
          <el-button text size="small" @click="capClicks = []">
            <el-icon style="margin-right: 4px"><RefreshLeft /></el-icon>
            重置
          </el-button>
          <span class="cap-tip">点错的字，点它的序号即可取消</span>
          <el-button text size="small" @click="newCaptcha">
            <el-icon style="margin-right: 4px"><RefreshRight /></el-icon>
            换一张
          </el-button>
        </div>

        <transition name="fade">
          <div v-if="capError" class="cap-error">
            <el-icon><CircleCloseFilled /></el-icon>
            {{ capError }}
          </div>
        </transition>

        <el-button
          type="primary"
          size="large"
          class="cap-btn"
          round
          :loading="capVerifying"
          :disabled="capClicks.length < CAPTCHA_NEED || capVerifying"
          @click="onCaptchaOk"
        >
          {{ capVerifying ? '正在验证…' : '确认解锁' }}
        </el-button>

        <div class="cap-meta">
          <el-icon><Clock /></el-icon>{{ capExpireRemain }}s 后自动更换
        </div>
      </div>
    </el-dialog>
  </div>
</template>
<style scoped>
.login-page {
  position: relative;
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  overflow: hidden;
  background:
    radial-gradient(circle at 20% 18%, rgba(30, 111, 224, 0.09), transparent 42%),
    radial-gradient(circle at 85% 80%, rgba(62, 207, 142, 0.08), transparent 40%),
    var(--ky-bg);
}
.login-page::before {
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
  width: 420px;
  height: 420px;
  top: -150px;
  left: -110px;
  background: #7fb3f2;
}
.glow-b {
  width: 380px;
  height: 380px;
  bottom: -160px;
  right: -90px;
  background: #8fe6c4;
}

/* ===== 外层双面卡片 ===== */
.flip-scene {
  position: relative;
  z-index: 1;
  width: 100%;
  max-width: 400px;
  perspective: 1500px;
  animation: card-rise 0.55s var(--ease-out) both;
}
@keyframes card-rise {
  from {
    opacity: 0;
    transform: translateY(16px) scale(0.985);
  }
}

.flip3d {
  position: relative;
  width: 100%;
  transform-style: preserve-3d;
  transition: transform 0.8s var(--ease-out);
}
.flip3d.flipped {
  transform: rotateY(180deg);
}

.face {
  backface-visibility: hidden;
  -webkit-backface-visibility: hidden;
}

.card-face {
  background: #fff;
  border-radius: 20px;
  box-shadow: 0 30px 70px rgba(15, 38, 74, 0.18);
  padding: 36px 34px 24px;
  text-align: center;
  /* 以"重新设置"面含报错信息的最高态为基准撑高卡片，正面内容垂直居中 */
  min-height: 630px;
  display: flex;
  flex-direction: column;
  justify-content: center;
}

/* 背面：绝对定位随正面高度撑开 */
.face-back {
  position: absolute;
  inset: 0;
  transform: rotateY(180deg);
}

/* ===== 背面内部的二次翻面 ===== */
.flip-inner {
  position: relative;
  width: 100%;
  height: 100%;
  transform-style: preserve-3d;
  transition: transform 0.8s var(--ease-out);
}
.flip-inner.flipped {
  transform: rotateY(180deg);
}

.inner-face {
  position: absolute;
  inset: 0;
  backface-visibility: hidden;
  -webkit-backface-visibility: hidden;
  display: flex;
  flex-direction: column;
  justify-content: center;
  overflow-y: auto;
  overflow-x: hidden;
  text-align: center;
}

/* 3D 翻面层内按钮阴影栅格化异常，独立合成层保证与正面一致 */
.inner-face .submit {
  transform: translateZ(0);
}

/* 登录面 / 找回面：拉开间距填满卡片高度（重新设置面保持紧凑正好） */
.pane-login h1,
.pane-key h1 {
  margin-top: 22px;
}
.pane-login .sub,
.pane-key .sub {
  margin-bottom: 36px;
}
.pane-login .field,
.pane-key .field {
  margin-bottom: 26px;
}
.pane-login .options-row {
  margin: 12px 0 14px;
}
.pane-login .submit,
.pane-key .submit {
  margin-top: 22px;
}
.pane-login .tips {
  margin-top: 30px;
  padding-top: 16px;
}
.pane-key .key-hero {
  width: 74px;
  height: 74px;
  margin-bottom: 10px;
  border-radius: 18px;
}
.pane-key .key-hero .el-icon {
  font-size: 34px;
}
.pane-key .key-hint {
  margin-top: 10px;
}
.pane-key .switch-line {
  margin-top: 24px;
}
.inner-backface {
  transform: rotateY(180deg);
}

.key-hero {
  width: 62px;
  height: 62px;
  margin: 0 auto 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 16px;
  background: var(--ky-gradient);
  box-shadow: 0 12px 26px rgba(30, 111, 224, 0.35);
}

.ok-icon {
  width: 62px;
  height: 62px;
  margin: 0 auto 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: #eef9f4;
  animation: pop-in 0.5s var(--ease-out) both;
}
@keyframes pop-in {
  from {
    transform: scale(0.4);
    opacity: 0;
  }
}

.logo {
  width: 56px;
  height: 56px;
  border-radius: 14px;
  box-shadow: 0 12px 26px rgba(23, 58, 105, 0.35);
}

h1 {
  margin: 14px 0 5px;
  font-size: 22px;
  letter-spacing: 2px;
}
.sub {
  margin: 0 0 20px;
  font-size: 12.5px;
  color: var(--ky-text-faint);
  line-height: 1.7;
}

/* 字段 */
.field {
  text-align: left;
  margin-bottom: 14px;
}
.field-label {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--ky-text-sub);
  margin-bottom: 6px;
}
.new-flag {
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--ky-primary-soft);
  color: var(--ky-primary);
  font-size: 10.5px;
  font-weight: 700;
}
.field :deep(.el-input) {
  width: 100%;
}

.is-error :deep(.el-input__wrapper) {
  box-shadow: 0 0 0 1px var(--el-color-danger) inset;
}

.error-line {
  display: flex;
  align-items: center;
  gap: 6px;
  justify-content: flex-start;
  margin-bottom: 12px;
  font-size: 12.5px;
  color: var(--el-color-danger);
}

.mismatch {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  font-size: 12.5px;
  color: #f56c6c;
}

.create-hint {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 8px 12px;
  margin-bottom: 12px;
  border-radius: 10px;
  background: #eef9f4;
  border: 1px solid #d3f0e4;
  color: #2e8f6d;
  font-size: 12px;
  text-align: left;
}

.key-input :deep(textarea) {
  font-family: 'JetBrains Mono', Consolas, monospace;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-size: 13px;
  line-height: 1.8;
  padding: 10px 14px;
}
.key-hint {
  margin-top: 6px;
  font-size: 11.5px;
  color: var(--ky-text-faint);
}

/* 强度条 */
.strength {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 10px;
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

/* 记住我 / 忘记密码 */
.options-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 2px 0 4px;
}
.forgot-link {
  font-size: 12.5px;
}

.submit {
  width: 100%;
  margin-top: 6px;
  letter-spacing: 2px;
  background: var(--ky-gradient);
  border: none;
  box-shadow: 0 10px 24px rgba(30, 111, 224, 0.35);
}
.submit:hover {
  filter: brightness(1.07);
}

.switch-line {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  margin-top: 14px;
  font-size: 13px;
  color: var(--ky-primary);
  cursor: pointer;
  user-select: none;
}
.switch-line:hover {
  opacity: 0.8;
}

/* 恢复密钥面板 */
.recovery-icon {
  width: 68px;
  height: 68px;
  margin: 0 auto 4px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 18px;
  background: var(--ky-gradient);
  box-shadow: 0 12px 28px rgba(30, 111, 224, 0.38);
}
.warn-text {
  color: #c2561a;
}
.recovery-key {
  margin: 0 0 20px;
  padding: 15px 16px;
  border-radius: 12px;
  background: #f5f8fc;
  border: 1.5px dashed #b9d3f0;
  font-family: 'JetBrains Mono', Consolas, monospace;
  font-size: 15px;
  font-weight: 700;
  letter-spacing: 1px;
  color: var(--ky-primary-deep);
  word-break: break-all;
  user-select: all;
}
.actions {
  display: flex;
  gap: 12px;
}
.actions .el-button {
  flex: 1;
}

/* ===== 弹窗（错误 / 验证码）===== */
.dlg-body {
  text-align: center;
  padding: 4px 6px 8px;
  position: relative;
  overflow: hidden;
}
.dlg-strip {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 5px;
  background: var(--ky-gradient);
}
.dlg-strip.warn {
  background: linear-gradient(90deg, #f5b860, #e08c2f);
}


.dlg-title {
  font-size: 17.5px;
  font-weight: 700;
  letter-spacing: 0.5px;
}
.dlg-desc {
  margin: 8px 0 14px;
  font-size: 13px;
  color: var(--ky-text-sub);
  line-height: 1.7;
}



/* 右上角叉号关闭按钮 */
.ky-dialog :deep(.el-dialog__headerbtn) {
  width: 34px;
  height: 34px;
  top: 10px;
  right: 10px;
}
.ky-dialog :deep(.el-dialog__close) {
  font-size: 19px;
  color: var(--ky-text-faint);
  transition: color 0.2s, transform 0.25s var(--ease-out);
}
.ky-dialog :deep(.el-dialog__headerbtn:hover .el-dialog__close) {
  color: var(--ky-primary);
  transform: rotate(90deg);
}

/* 汉字点选验证码 */
.cap-click-wrap {
  position: relative;
  border-radius: 12px;
  border: 1px solid var(--ky-border);
  overflow: hidden;
  cursor: crosshair;
  margin-bottom: 8px;
  transition: border-color 0.2s, box-shadow 0.2s;
}
.cap-click-wrap:hover {
  border-color: var(--el-color-primary-light-5);
  box-shadow: 0 6px 16px rgba(30, 111, 224, 0.14);
}
.cap-click-img {
  width: 100%;
  height: 120px;
  display: block;
  user-select: none;
  -webkit-user-drag: none;
  pointer-events: none;
}
.cap-mark {
  position: absolute;
  width: 22px;
  height: 22px;
  margin: -11px 0 0 -11px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  background: var(--ky-primary);
  color: #fff;
  font-size: 12px;
  font-weight: 700;
  box-shadow: 0 2px 8px rgba(30, 111, 224, 0.4);
  animation: mark-pop 0.22s var(--ease-out) both;
  pointer-events: auto;
  cursor: pointer;
  transition: background 0.18s ease, transform 0.18s ease;
}
.cap-mark:hover {
  background: #e05252;
  transform: scale(1.15);
}
@keyframes mark-pop {
  from {
    transform: scale(0.4);
    opacity: 0;
  }
}
.cap-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}
.cap-tip {
  font-size: 11.5px;
  color: var(--ky-text-faint);
}
.cap-click-wrap.verifying {
  cursor: wait;
}
.cap-ready {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  padding: 4px 0;
  background: rgba(30, 111, 224, 0.88);
  color: #fff;
  font-size: 12px;
  letter-spacing: 1px;
}
.cap-error {
  display: flex;
  align-items: center;
  gap: 6px;
  justify-content: flex-start;
  margin: 10px 0 0;
  font-size: 12.5px;
  color: var(--el-color-danger);
}

.cap-meta {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 5px;
  margin-top: 12px;
  font-size: 12px;
  color: var(--ky-text-faint);
}

.cap-btn {
  width: 100%;
  margin-top: 10px;
  background: var(--ky-gradient);
  border: none;
  box-shadow: 0 10px 24px rgba(30, 111, 224, 0.35);
}

/* 冷却态解锁按钮 */
.submit.cooling {
  background: linear-gradient(135deg, #c3cad6 0%, #98a3b3 100%);
  box-shadow: none;
  letter-spacing: 1px;
  font-family: 'JetBrains Mono', Consolas, monospace;
}

.tips {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px dashed var(--ky-border);
  font-size: 12px;
  color: var(--ky-text-faint);
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.25s;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
.slide-enter-active {
  transition: all 0.35s var(--ease-out);
  overflow: hidden;
}
.slide-enter-from {
  opacity: 0;
  max-height: 0;
  margin-bottom: -14px;
}

@media (max-width: 480px) {
  .card-face {
    padding: 30px 20px 20px;
  }
  .login-page {
    padding: 16px;
  }
}
</style>
