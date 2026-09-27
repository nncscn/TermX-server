// 整体以标准 Base64 传输），同一把数据密钥两端互通。
//

/* ---------------- Base64 / Hex ---------------- */

export function bytesToB64(bytes) {
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
  return btoa(bin)
}

export function b64ToBytes(b64) {
  const bin = atob(b64.replace(/-/g, '+').replace(/_/g, '/'))
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function hexToBytes(hex) {
  const out = new Uint8Array(hex.length / 2)
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16)
  return out
}

/**
 * 归一化数据密钥输入：Base64（43/44 字符）或 Hex（64 字符）→ 32 字节。
 * 不合法时抛错（供表单校验提示）。
 */
// TODO: HTTPS后换crypto.subtle
export function normalizeKey(input) {
  const s = String(input || '').trim()
  if (/^[0-9a-fA-F]{64}$/.test(s)) return hexToBytes(s)
  if (/^[A-Za-z0-9+/]{43}={0,1}$/.test(s) || /^[A-Za-z0-9_-]{43}$/.test(s)) {
    const b = b64ToBytes(s)
    if (b.length === 32) return b
  }
  throw new Error('密钥格式不正确（需 Base64 43 字符或 64 位 Hex）')
}

/* ---------------- AES-256 核心 ---------------- */

const SBOX = new Uint8Array(256)
;(() => {
  // 生成 S 盒（GF(2^8) 求逆 + 仿射变换）
  const p = new Uint8Array(256)
  const l = new Uint8Array(256)
  let x = 1
  for (let i = 0; i < 255; i++) {
    p[i] = x // pow 表
    l[x] = i // log 表
    x ^= (x << 1) ^ (x & 0x80 ? 0x11b : 0)
    x &= 0xff
  }
  for (let i = 0; i < 256; i++) {
    const inv = i === 0 ? 0 : p[(255 - l[i]) % 255]
    SBOX[i] =
      (inv ^ ((inv << 1) | (inv >> 7)) ^ ((inv << 2) | (inv >> 6)) ^
        ((inv << 3) | (inv >> 5)) ^ ((inv << 4) | (inv >> 4)) ^ 0x63) & 0xff
  }
})()

const RCON = [0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x1b, 0x36, 0x6c]

/** AES-256 密钥扩展：返回 15 个轮密钥（每轮 16 字节，共 240 字节） */
function expandKey(key) {
  const Nk = 8
  const Nr = 14
  const w = new Uint8Array(16 * (Nr + 1))
  w.set(key)
  let rconIdx = 0
  for (let i = Nk; i < 4 * (Nr + 1); i++) {
    let t = [w[(i - 1) * 4], w[(i - 1) * 4 + 1], w[(i - 1) * 4 + 2], w[(i - 1) * 4 + 3]]
    if (i % Nk === 0) {
      t = [SBOX[t[1]] ^ RCON[rconIdx++], SBOX[t[2]], SBOX[t[3]], SBOX[t[0]]]
    } else if (i % Nk === 4) {
      t = [SBOX[t[0]], SBOX[t[1]], SBOX[t[2]], SBOX[t[3]]]
    }
    for (let j = 0; j < 4; j++) w[i * 4 + j] = w[(i - Nk) * 4 + j] ^ t[j]
  }
  return w
}

const xtime = (a) => ((a << 1) ^ (a & 0x80 ? 0x1b : 0)) & 0xff

/** AES 单块加密（16 字节 in → 16 字节 out） */
function encryptBlock(w, input) {
  const s = new Uint8Array(input)
  // AddRoundKey 0
  for (let i = 0; i < 16; i++) s[i] ^= w[i]
  for (let round = 1; round <= 14; round++) {
    // SubBytes
    for (let i = 0; i < 16; i++) s[i] = SBOX[s[i]]
    // ShiftRows
    let t = s[1]; s[1] = s[5]; s[5] = s[9]; s[9] = s[13]; s[13] = t
    t = s[2]; s[2] = s[10]; s[10] = t; t = s[6]; s[6] = s[14]; s[14] = t
    t = s[15]; s[15] = s[11]; s[11] = s[7]; s[7] = s[3]; s[3] = t
    // MixColumns（末轮跳过）
    if (round < 14) {
      for (let c = 0; c < 4; c++) {
        const o = c * 4
        const a0 = s[o], a1 = s[o + 1], a2 = s[o + 2], a3 = s[o + 3]
        s[o] = xtime(a0) ^ (xtime(a1) ^ a1) ^ a2 ^ a3
        s[o + 1] = a0 ^ xtime(a1) ^ (xtime(a2) ^ a2) ^ a3
        s[o + 2] = a0 ^ a1 ^ xtime(a2) ^ (xtime(a3) ^ a3)
        s[o + 3] = (xtime(a0) ^ a0) ^ a1 ^ a2 ^ xtime(a3)
      }
    }
    // AddRoundKey
    const off = round * 16
    for (let i = 0; i < 16; i++) s[i] ^= w[off + i]
  }
  return s
}

/* ---------------- GCM ---------------- */

/** GF(2^128) 域乘（位串右移算法，NIST SP 800-38D） */
function gfMul(x, y) {
  const Z = new Uint8Array(16)
  const V = new Uint8Array(y)
  for (let i = 0; i < 128; i++) {
    if ((x[i >> 3] & (0x80 >> (i & 7))) !== 0) {
      for (let k = 0; k < 16; k++) Z[k] ^= V[k]
    }
    const lsb = V[15] & 1
    // V 右移 1 位
    for (let k = 15; k > 0; k--) V[k] = (V[k] >> 1) | ((V[k - 1] & 1) << 7)
    V[0] >>= 1
    if (lsb) V[0] ^= 0xe1
  }
  return Z
}

function ghash(h, aad, data) {
  const y = new Uint8Array(16)
  const blocks = []
  for (let i = 0; i < aad.length; i += 16) {
    const b = new Uint8Array(16)
    b.set(aad.subarray(i, i + 16))
    blocks.push(b)
  }
  for (let i = 0; i < data.length; i += 16) {
    const b = new Uint8Array(16)
    b.set(data.subarray(i, i + 16))
    blocks.push(b)
  }
  const len = new Uint8Array(16)
  const ab = BigInt(aad.length * 8)
  const db = BigInt(data.length * 8)
  for (let i = 0; i < 8; i++) {
    len[i] = Number((ab >> BigInt(8 * (7 - i))) & 0xffn)
    len[8 + i] = Number((db >> BigInt(8 * (7 - i))) & 0xffn)
  }
  blocks.push(len)
  for (const b of blocks) {
    for (let k = 0; k < 16; k++) y[k] ^= b[k]
    y.set(gfMul(y, h))
  }
  return y
}

function inc32(counter) {
  const c = new Uint8Array(counter)
  for (let i = 15; i >= 12; i--) {
    c[i] = (c[i] + 1) & 0xff
    if (c[i] !== 0) break
  }
  return c
}

function ctrXor(w, j0, data) {
  const out = new Uint8Array(data.length)
  let ctr = j0
  for (let i = 0; i < data.length; i += 16) {
    ctr = inc32(ctr)
    const ks = encryptBlock(w, ctr)
    const n = Math.min(16, data.length - i)
    for (let k = 0; k < n; k++) out[i + k] = data[i + k] ^ ks[k]
  }
  return out
}

/** AES-256-GCM 加密：返回 nonce(12) ‖ 密文 ‖ tag(16) */
function gcmEncrypt(keyBytes, plaintext, nonce) {
  const w = expandKey(keyBytes)
  const h = encryptBlock(w, new Uint8Array(16))
  const j0 = new Uint8Array(16)
  j0.set(nonce)
  j0[15] = 1
  const cipher = ctrXor(w, j0, plaintext)
  const s = ghash(h, new Uint8Array(0), cipher)
  const tagMask = encryptBlock(w, j0)
  const tag = new Uint8Array(16)
  for (let i = 0; i < 16; i++) tag[i] = s[i] ^ tagMask[i]
  const out = new Uint8Array(12 + cipher.length + 16)
  out.set(nonce, 0)
  out.set(cipher, 12)
  out.set(tag, 12 + cipher.length)
  return out
}

/** AES-256-GCM 解密：校验 tag，失败返回 null */
function gcmDecrypt(keyBytes, blob) {
  if (blob.length < 28) return null
  const w = expandKey(keyBytes)
  const h = encryptBlock(w, new Uint8Array(16))
  const nonce = blob.subarray(0, 12)
  const cipher = blob.subarray(12, blob.length - 16)
  const tag = blob.subarray(blob.length - 16)
  const j0 = new Uint8Array(16)
  j0.set(nonce)
  j0[15] = 1
  const s = ghash(h, new Uint8Array(0), cipher)
  const tagMask = encryptBlock(w, j0)
  let diff = 0
  for (let i = 0; i < 16; i++) diff |= (s[i] ^ tagMask[i] ^ tag[i]) & 0xff
  if (diff !== 0) return null
  return ctrXor(w, j0, cipher)
}

function randomBytes(n) {
  const b = new Uint8Array(n)
  if (window.crypto && window.crypto.getRandomValues) {
    window.crypto.getRandomValues(b) // 随机数不要求安全上下文
  } else {
    for (let i = 0; i < n; i++) b[i] = (Math.random() * 256) | 0
  }
  return b
}

/* ---------------- 对外接口 ---------------- */

/**
 * 加密任意可 JSON 序列化对象 → Base64 密文（nonce‖ct‖tag）。
 * @param {string} keyInput 数据密钥（Base64/Hex）
 * @param {any} obj 明文对象
 */
export function encryptData(keyInput, obj) {
  let key
  try {
    key = normalizeKey(keyInput)
  } catch {
    throw new Error('数据密钥不可用，请重新登录获取')
  }
  const plaintext = new TextEncoder().encode(JSON.stringify(obj))
  return bytesToB64(gcmEncrypt(key, plaintext, randomBytes(12)))
}

/**
 * 解密 Base64 密文 → 明文对象。密钥不正确或数据损坏时抛出友好错误。
 * @param {string} keyInput 数据密钥
 * @param {string} blobB64 Base64 密文
 */
export function decryptData(keyInput, blobB64) {
  let plain
  try {
    plain = gcmDecrypt(normalizeKey(keyInput), b64ToBytes(blobB64))
  } catch {
    throw new Error('密钥不正确')
  }
  if (!plain) throw new Error('密钥不正确')
  const text = new TextDecoder().decode(plain)
  return JSON.parse(text)
}

/* ---------------- SHA-256（指纹计算） ---------------- */

const K256 = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
]

export function sha256(data) {
  const l = data.length
  const bitLenHi = Math.floor((l * 8) / Math.pow(2, 32))
  const bitLenLo = (l * 8) >>> 0
  const padded = new Uint8Array((((l + 8) >> 6) + 1) * 64)
  padded.set(data)
  padded[l] = 0x80
  const dv = padded.length - 8
  padded[dv] = (bitLenHi >>> 24) & 0xff; padded[dv + 1] = (bitLenHi >>> 16) & 0xff
  padded[dv + 2] = (bitLenHi >>> 8) & 0xff; padded[dv + 3] = bitLenHi & 0xff
  padded[dv + 4] = (bitLenLo >>> 24) & 0xff; padded[dv + 5] = (bitLenLo >>> 16) & 0xff
  padded[dv + 6] = (bitLenLo >>> 8) & 0xff; padded[dv + 7] = bitLenLo & 0xff
  let h0 = 0x6a09e667, h1 = 0xbb67ae85, h2 = 0x3c6ef372, h3 = 0xa54ff53a
  let h4 = 0x510e527f, h5 = 0x9b05688c, h6 = 0x1f83d9ab, h7 = 0x5be0cd19
  const w = new Int32Array(64)
  const rr = (x, n) => (x >>> n) | (x << (32 - n))
  for (let off = 0; off < padded.length; off += 64) {
    for (let i = 0; i < 16; i++) {
      const j = off + i * 4
      w[i] = (padded[j] << 24) | (padded[j + 1] << 16) | (padded[j + 2] << 8) | padded[j + 3]
    }
    for (let i = 16; i < 64; i++) {
      const s0 = rr(w[i - 15], 7) ^ rr(w[i - 15], 18) ^ (w[i - 15] >>> 3)
      const s1 = rr(w[i - 2], 17) ^ rr(w[i - 2], 19) ^ (w[i - 2] >>> 10)
      w[i] = (w[i - 16] + s0 + w[i - 7] + s1) | 0
    }
    let a = h0, b = h1, c = h2, d = h3, e = h4, f = h5, g = h6, hh = h7
    for (let i = 0; i < 64; i++) {
      const S1 = rr(e, 6) ^ rr(e, 11) ^ rr(e, 25)
      const ch = (e & f) ^ (~e & g)
      const t1 = (hh + S1 + ch + K256[i] + w[i]) | 0
      const S0 = rr(a, 2) ^ rr(a, 13) ^ rr(a, 22)
      const maj = (a & b) ^ (a & c) ^ (b & c)
      const t2 = (S0 + maj) | 0
      hh = g; g = f; f = e; e = (d + t1) | 0
      d = c; c = b; b = a; a = (t1 + t2) | 0
    }
    h0 = (h0 + a) | 0; h1 = (h1 + b) | 0; h2 = (h2 + c) | 0; h3 = (h3 + d) | 0
    h4 = (h4 + e) | 0; h5 = (h5 + f) | 0; h6 = (h6 + g) | 0; h7 = (h7 + hh) | 0
  }
  const out = new Uint8Array(32)
  const hs = [h0, h1, h2, h3, h4, h5, h6, h7]
  for (let i = 0; i < 8; i++) {
    out[i * 4] = (hs[i] >>> 24) & 0xff
    out[i * 4 + 1] = (hs[i] >>> 16) & 0xff
    out[i * 4 + 2] = (hs[i] >>> 8) & 0xff
    out[i * 4 + 3] = hs[i] & 0xff
  }
  return out
}

/** 由 OpenSSH 公钥行计算 SHA256 指纹（与 ssh-keygen -lf 一致；无公钥返回空串） */
export function fingerprintOfPublicKey(publicKey) {
  const fields = String(publicKey || '').trim().split(/\s+/)
  if (fields.length < 2) return ''
  try {
    const blob = b64ToBytes(fields[1])
    const sum = sha256(blob)
    let bin = ''
    for (let i = 0; i < sum.length; i++) bin += String.fromCharCode(sum[i])
    return 'SHA256:' + btoa(bin).replace(/=+$/, '')
  } catch {
    return ''
  }
}

/* ---------------- 会话数据密钥（内存缓存，刷新即失，登录重新获取） ---------------- */

let sessionDataKey = ''

/**
 * 保存本次会话的数据密钥（内存 + sessionStorage 兜底：整页刷新后仍可用，
 * 关闭浏览器标签即清空）。@param {string} k Base64 密钥
 */
export function setSessionDataKey(k) {
  sessionDataKey = String(k || '')
  try {
    if (sessionDataKey) sessionStorage.setItem('ky_data_key', sessionDataKey)
    else sessionStorage.removeItem('ky_data_key')
  } catch {
    /* 隐私模式下 sessionStorage 不可用时仅内存 */
  }
}

/** 读取数据密钥：内存优先，回退 sessionStorage（整页刷新后） */
export function getSessionDataKey() {
  if (sessionDataKey) return sessionDataKey
  try {
    sessionDataKey = sessionStorage.getItem('ky_data_key') || ''
  } catch {
    /* ignore */
  }
  return sessionDataKey
}

/* ---------------- NIST 官方向量自检（加载即验，失败在控制台红字） ---------------- */

;(() => {
  try {
    const key = new Uint8Array(32) // 全零密钥/IV
    const iv = new Uint8Array(12)
    const t1 = gcmEncrypt(key, new Uint8Array(0), iv)
    let hex = ''
    for (const b of t1) hex += b.toString(16).padStart(2, '0')
    if (hex !== '000000000000000000000000530f8afbc74536b9a963b4f1c4cb738b') {
      throw new Error('GCM 向量1失败: ' + hex)
    }
    const t2 = gcmEncrypt(key, new Uint8Array(16), iv)
    hex = ''
    for (const b of t2) hex += b.toString(16).padStart(2, '0')
    if (hex !== '000000000000000000000000cea7403d4d606b6e074ec5d3baf39d18d0d1c8a799996bf0265b98b5d48ab919') {
      throw new Error('GCM 向量2失败: ' + hex)
    }
    // SHA-256("abc") = ba7816bf…
    const d = sha256(new TextEncoder().encode('abc'))
    hex = ''
    for (const b of d) hex += b.toString(16).padStart(2, '0')
    if (!hex.startsWith('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')) {
      throw new Error('SHA256 向量失败: ' + hex)
    }
    // 往返
    const roundtrip = gcmDecrypt(key, gcmEncrypt(key, new TextEncoder().encode('往返测试'), randomBytes(12)))
    if (new TextDecoder().decode(roundtrip) !== '往返测试') throw new Error('往返失败')
    console.log('[ky] 客户端加密自检通过（AES-256-GCM NIST 向量 + SHA-256）')
  } catch (e) {
    console.error('[ky] 客户端加密自检失败：' + e.message)
  }
})()
