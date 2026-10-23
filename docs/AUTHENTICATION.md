# 身份认证与安全

## 1. 身份模型

Worklog 是受保护的**单用户服务**，没有注册、登录页面、用户管理或 OAuth 授权服务器。  
所有受信客户端访问 `WORKLOG_USER_ID` 指定的工作台（默认 `owner`），不能通过请求体、工具参数或 `X-Forwarded-User` 选择用户。

浏览器 Basic、Bearer 读写令牌和 stdio 是独立的认证渠道，不是不同用户账号。  
内部数据按用户标识隔离不等于已经提供多用户产品；不要把更改 `WORKLOG_USER_ID` 当作登录切换功能。

## 2. 认证渠道与权限

| 渠道 | 读取 | 普通写入 | 删除／清空等破坏性操作 |
| --- | --- | --- | --- |
| 本地匿名 REST | 是 | 是 | 是，兼容默认 UI |
| REST 浏览器 Basic | 是 | 是 | 是 |
| REST Bearer 写令牌 | 是 | 是 | 否，不受 MCP 开关影响 |
| REST / MCP Bearer 只读令牌 | 是 | 否 | 否 |
| MCP HTTP Bearer 写令牌 | 是 | 是 | 默认否，运维开启后允许 |
| stdio | 是 | 默认是 | 默认否，运维开启后允许；只读模式下仍禁止 |

普通写权限允许修改已有正文和字段，**不保证信息不会丢失**。当前没有日志历史或回收站，不能用“禁止删除”替代备份。

### 本地匿名 REST

只有以下条件同时满足才允许：

- Go 服务绑定 loopback 地址。
- 未设置 `WORKLOG_BASIC_PASSWORD`。
- 请求没有 `Authorization` 头。
- 请求 Host 和实际远端地址都是 loopback。
- 请求通过 Host / Origin 等安全检查。

经本地代理转发但保留公网域名 Host 的请求不会获得匿名权限。  
设置 Basic 密码会关闭匿名 REST；给出错误的 Authorization 也不会退回匿名模式。

**同机其他进程也能访问匿名 REST。** 这不是同机 agent 沙箱。  
即使配置密码，能读取 `.env`、令牌或数据库的进程仍可绕过预期隔离，需要 OS 用户、文件权限或容器边界配合。

### 浏览器 Basic

```sh
export WORKLOG_BASIC_USER=worklog
export WORKLOG_BASIC_PASSWORD="$(openssl rand -hex 24)"
```

用户名默认 `worklog`；非空密码至少 **8 字节**。  
浏览器弹出原生认证提示，前端不保存令牌，不新增登录 UI。Basic 可读写和删除，面向人类浏览器的删除仍使用现有确认交互。

Basic 不是加密方式，只能经 HTTPS 用于远程访问。  
浏览器可能缓存凭证；共享设备使用独立浏览器配置并在使用后关闭会话。MCP 不接受 Basic。

### Bearer 令牌

```sh
export WORKLOG_WRITE_TOKEN="$(openssl rand -hex 32)"
export WORKLOG_READ_TOKEN="$(openssl rand -hex 32)"  # 可选
```

两个令牌均至少 **32 字节**，且必须不同。它们也应与 Basic 密码独立。  
支持 `Authorization: Bearer <token>`（scheme 不区分大小写），不要放在 URL、工具参数、前端 `config.js` 或源码中。

本地未配置写令牌时，服务自动读取／生成 `WORKLOG_TOKEN_FILE`（默认 `./data/mcp-token`）。文件以 0600 权限创建，不输出令牌到日志。  
非 loopback 绑定必须显式设置写令牌和 Basic 密码，不能依赖自动生成绕过启动要求。显式设置写令牌后，令牌文件不是该次服务的有效凭证来源。

REST 和 MCP 可共用写令牌，但 REST Bearer 不获得删除／清空权限。只读 AI 应使用独立的 `WORKLOG_READ_TOKEN`。

调用示例（令牌从安全环境注入）：

```sh
curl https://worklog.example.com/api/today \
  -H "Authorization: Bearer $WORKLOG_READ_TOKEN"
```

不要在日志、截图或共享终端中输出凭证。HTTP MCP 配置见 [MCP 指南](MCP.md)。

### stdio

stdio 由启动进程的可信本机 OS 用户及环境配置提供身份，不要求 HTTP Bearer，也不接受模型指定 userId。  
该进程直接访问数据库，启动它本身就是授权行为。

- `WORKLOG_STDIO_READ_ONLY=true`：强制只读。
- `WORKLOG_MCP_ALLOW_DESTRUCTIVE=true`：允许具有写权限的 stdio / MCP 客户端执行破坏性操作。
- stdout 仅输出 MCP 协议，日志写入 stderr。

不要把包含数据库访问能力的 stdio 配置交给不可信用户；限制数据库目录和进程环境的读取权限。

## 3. 破坏性授权

默认 MCP 禁止删除项目／任务／待办、批量分类管理及保存空日志。模型给出 `confirm: true` 或文字确认都不能解锁。

运维可在可信服务端环境设置：

```sh
export WORKLOG_MCP_ALLOW_DESTRUCTIVE=true
# 重启服务后生效
```

这是**长期能力授权，不是逐次用户审批**：开启后，该服务的 MCP 写令牌持有人即具备对应能力，所有连接该服务的写客户端都会受影响。  
开启前先备份；需要逐次人工审批时应使用外部授权网关。此开关不扩大 REST Bearer 权限，也不会使只读令牌可写。

本地匿名 REST 仍可删除，因此不能靠 MCP 开关限制有本机 REST 访问能力的 agent。

## 4. Host、Origin 与反向代理

- `WORKLOG_ALLOWED_HOSTS`：逗号分隔主机名，不含协议和端口，例如 `worklog.example.com,localhost,127.0.0.1`。
- `WORKLOG_ALLOWED_ORIGINS`：逗号分隔的**精确 Origin**，包含协议及非默认端口，不带路径或末尾 `/`，例如 `https://worklog.example.com`。
- 直接同源请求可通过同源检查；TLS 在 Nginx 终止后，Go 看到的是 HTTP，因此必须显式加入外部 HTTPS Origin。
- 保留原始 Host、Origin、Authorization，不要将 Host 改成 `127.0.0.1` 来绕过检查。
- 不信任 `X-Forwarded-User` 等身份头，也不以 `X-Forwarded-Proto` 作为认证依据。

Host / Origin 白名单是请求可信性检查，不是登录凭证。CORS 也不授予身份。  
推荐浏览器与 API 同源：前端使用 `credentials: same-origin`，不提供跨域 Cookie 或跨域登录机制。  
跨域 JSON API 仅向可信 Origin 返回 CORS 头，支持 PUT / PATCH 等预检，预检缓存为 600 秒。

写请求要求 JSON。无 Origin 的 cross-site 请求仅允许 GET / HEAD 顶层页面导航；跨站 fetch 或子资源不能借用本机／Basic 凭证。  
`/healthz` 无需认证但仍校验 Host，只返回数据库可用状态。

Nginx 示例见[部署与配置](DEPLOYMENT.md#nginx-https-反向代理)。  
不要另外配置覆盖 `/mcp` 的 Nginx Basic 认证，否则客户端无法同时提交 Bearer 认证。

## 5. 凭证保管与轮换

- `.env` 建议 0600，数据目录以 0700、数据库／令牌／备份以 0600 创建；已有目录不会自动修正权限。
- 使用独立随机凭证，不把凭证提交到仓库或放入可公开读取的静态目录。
- 更新环境变量后重启服务，再更新客户端。当前每类令牌仅支持一个有效值，没有双令牌平滑轮换机制。
- 使用自动令牌文件时，停止相关服务、在安全位置替换令牌文件并保持 0600，再重启和更新客户端。若设置了 `WORKLOG_WRITE_TOKEN`，应轮换该环境变量而不是只替换文件。
- 泄露后应立即轮换并检查访问日志；泄露数据库或备份时，仅轮换令牌不能撤回已经泄露的数据。

## 6. 限额、日志与公网防护

- 工作台请求体 10 MiB，日志 1 MiB，MCP envelope 2 MiB（其中日志内容仍限 1 MiB）。
- 服务操作超时 12 秒，HTTP 读 15 秒／写 20 秒，header 上限 32 KiB。
- 认证后的 REST API / MCP HTTP 共享进程级 32 并发、每分钟 300 次调用预算，每个 HTTP 请求计一次；stdio 使用独立进程的同等工具层预算。
- 静态文件、认证失败和健康检查不消耗业务预算；健康检查另限 4 并发。
- 413 表示正文过大，429 表示调用过频，繁忙／超时可返回 503；超时不保证事务未提交。

业务预算**不是防暴力破解**。公网应在 Nginx 或入口网关另配请求限速／认证失败封禁，并按 MCP 和浏览器流量调整阈值。  
不要把 Authorization、请求正文或完整数据库 URL 加入代理日志。  
应用错误响应脱敏并含请求 ID；服务日志不记录私人正文、令牌或 SQL，MCP 审计只记录工具、可信身份、写入类型、结果和耗时等元数据。

## 7. 常见问题

| 现象 | 检查项 |
| --- | --- |
| 401 | Basic 用户／密码是否正确；Bearer 是否属于当前实例；是否误用了旧令牌文件；MCP 不能使用 Basic |
| 403 | Host / Origin 白名单是否匹配；是否尝试用只读或 REST Bearer 执行写入／删除 |
| 本机访问无需密码 | 是否未配置 Basic，且符合本地匿名条件 |
| 代理后无法保存 | 是否保留 Origin / Host，并配置准确的 HTTPS Origin |
| MCP 无法删除 | 默认策略如此；确认确有需要、完成备份后，由运维决定是否授权，而不是添加工具确认参数 |
