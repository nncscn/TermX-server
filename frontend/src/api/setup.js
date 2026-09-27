// 安装引导 API（公开接口；仅系统未初始化时可用，已初始化后后端返回 40301）
import { request } from './client'

/** 查询系统是否已完成初始化 GET /api/v1/setup/status */
export function getSetupStatus() {
  return request('/api/v1/setup/status', { auth: false })
}

/** 数据库连通测试 POST /api/v1/setup/test-connection（不建库、不落盘） */
export function testConnection(form) {
  return request('/api/v1/setup/test-connection', {
    method: 'POST',
    auth: false,
    body: {
      engine: form.engine,
      host: form.host,
      port: Number(form.port),
      user: form.username,
      password: form.password,
      name: form.database
    }
  })
}

/**
 * 服务配置可行性验证 POST /api/v1/setup/test-server：
 * 监听地址端口可绑定（占用/地址不存在/无权限分类提示）+ HTTPS 证书对校验
 */
export function testServer(srv) {
  return request('/api/v1/setup/test-server', {
    method: 'POST',
    auth: false,
    body: {
      host: srv.host,
      port: Number(srv.port),
      https: !!srv.https,
      cert_file: srv.certPath || '',
      key_file: srv.keyPath || ''
    }
  })
}

/**
 * 提交完整安装 POST /api/v1/setup/complete：
 * 后端原子执行 建库→建表→建账户→写配置→自重启；响应先行返回
 * { recovery_key, access_url, phases }（前端轮询 status 等待重启完成）。
 */
export function completeSetup(config) {
  return request('/api/v1/setup/complete', {
    method: 'POST',
    auth: false,
    body: config
  })
}
