// 仅在前端由独立静态服务器提供时，编辑构建产物 dist/config.js 即可，无需重新构建。
// 若页面由 Go 服务提供，Go 会动态响应 /config.js；此文件不会被读取。
// apiBaseUrl 是 API 基础地址，须包含 /api；未设置时使用构建时 VITE_API_BASE_URL。
// 独立静态部署示例（按实际后端地址和业务时区修改）：
// window.WORKLOG_CONFIG = {
//   apiBaseUrl: 'https://api.example.com/api',
//   businessTimezone: 'Asia/Shanghai'
// }
// 同源部署可将 apiBaseUrl 设为 '/api'；空字符串表示未配置后端。
// 当前前端不会向跨源 API 发送认证凭证或注入令牌；需认证时推荐通过反向代理让前端和 API 同源。
window.WORKLOG_CONFIG = {}
