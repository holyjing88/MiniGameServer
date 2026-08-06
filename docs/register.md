# 登录 / 注册协议

## 账号唯一键

```
account_id = "{channel_id}_{open_id}"
```

同一 `open_id` 在不同渠道是**不同账号**（如 `tiktok_minis_xxx` ≠ `weixin_xxx`）。

Session 内 `player_id` = `account_id`；榜单写入也使用该 ID。

## 流程

```
客户端                          服务器
   |  POST /v1/auth/login          |
   |  {app_id, channel_id, code}   |
   |------------------------------>|
   |                               | resolve code → open_id
   |                               | 查 (app_id, channel, open_id)
   |  404 NEED_REGISTER            | 未注册
   |  {open_id, account_id, ...}   |
   |<------------------------------|
   |  POST /v1/auth/register       |
   |  {app_id, channel_id, code}   |
   |------------------------------>|
   |  200 {is_new, need_login}     |
   |<------------------------------|
   |  POST /v1/auth/login          |
   |------------------------------>|
   |  200 {session_token, ...}     |
   |<------------------------------|
```

已注册用户直接 login 成功，跳过 register。

## HTTP

### `POST /v1/auth/login`（别名：`/v1/session`）

```json
{
  "app_id": "parking_smart_brain",
  "channel_id": "tiktok_minis",
  "code": "mock:<open_id>"
}
```

成功：

```json
{
  "session_token": "...",
  "expires_in": 86400,
  "player_id": "tiktok_minis_oid",
  "account_id": "tiktok_minis_oid",
  "open_id": "oid",
  "username": "玩家昵称",
  "click_id": "...",
  "channel": "tiktok_minis",
  "channel_id": "tiktok_minis"
}
```

未注册（HTTP 404）：

```json
{
  "code": "NEED_REGISTER",
  "msg": "account not registered; call /v1/auth/register then login",
  "open_id": "oid",
  "channel_id": "tiktok_minis",
  "account_id": "tiktok_minis_oid",
  "app_id": "parking_smart_brain"
}
```

### `POST /v1/auth/register`

**不需要** Session。用 `code` 解析 `open_id` 后建档；客户端弹窗收集 **username**，并传 **clickid**（推广归因，首注锁定）。

```json
{
  "app_id": "parking_smart_brain",
  "channel_id": "tiktok_minis",
  "code": "mock:<open_id>",
  "username": "玩家昵称",
  "clickid": "<ttclid 或投放 clickid>",
  "platform_kind": 1
}
```

字段别名：`clickid` / `click_id` / `ttclid` 均可。空值允许（自然量）。重复注册不覆盖已有 `click_id`（first-touch）。

```json
{
  "is_new": true,
  "need_login": true,
  "username": "玩家昵称",
  "click_id": "...",
  "open_id": "oid",
  "account_id": "tiktok_minis_oid",
  ...
}
```

登录成功响应含 `username` / `nickname`、`avatar_url` / `avatar`、`click_id`；客户端右上角展示昵称与头像。

### `app_id` 要不要传？

| 接口 | 要不要 body/query 传 `app_id` |
|------|-------------------------------|
| `POST /v1/auth/login` / `register` | **要**（游戏业务 app_id，如 `parking_smart_brain`） |
| `GET/POST /v1/player/profile` | **不要**（从 Session 取） |
| `POST /v1/player/profile/sync` | **不要**（从 Session 取） |

注意：TikTok 平台 **App ID**（portal 数字 ID）≠ HTTP 里的游戏业务 `app_id`（如 `parking_smart_brain`）。  
平台 `client_key` / portal `app_id` 由**客户端按游戏传入**；`client_secret` 只配在服务器 `RANK_TT_CLIENT_SECRETS`，**不要**下发客户端。

### `POST /api/v1/tiktok/profile`（别名：`/api/v1/auth/tt_profile`；兼容旧路径 `/v1/...`）

**不需要** Session。把 TikTok `authorize` / `login` 返回的一次性 `code` 换票并拉昵称头像。  
供尚未接入 MiniGameServer 登录、只做主界面头像的游戏使用（如 VampireDormitory）。

所有 HTTP 接口规范前缀为 **`/api`**（例如登录为 `/api/v1/auth/login`）。

```json
{
  "auth_code": "<TTMinis.game.authorize / login 返回的 code>",
  "channel_id": "tiktok_minis",
  "client_key": "<该游戏 TikTok client_key，必填>",
  "app_id": "<TikTok portal App ID，可选>",
  "tt_app_id": "<tt… 小游戏 appid，可选>",
  "mini_app_id": "<业务 app_id，如 parking_smart_brain，可选>"
}
```

字段别名：`auth_code`（推荐）/ `tt_code` / `code`。  
Cocos 老 `HttpUnit.request_csryw` 会覆盖 body.`code`，客户端应传 **`auth_code`**。

**多游戏**：`client_key` / `app_id` 由**各游戏客户端传入**；服务端用 body 的 `client_key` 在 `RANK_TT_CLIENT_SECRETS` 中查找对应 `client_secret` 换票。  
**不要**在服务端写死单一 `RANK_TT_CLIENT_KEY` 给所有游戏用。`client_secret` 仅存服务端。

成功：

```json
{
  "open_id": "...",
  "nickname": "...",
  "username": "...",
  "avatar_url": "https://...",
  "avatar": "https://...",
  "channel_id": "tiktok_minis",
  "source": "oauth"
}
```

失败：`{"code":"TT_PROFILE_FAIL","msg":"..."}`  

前置：`RANK_AUTH_MODE` 含 `tiktok`，并配置 `RANK_TT_CLIENT_SECRETS`（或兼容旧的单组 `RANK_TT_CLIENT_KEY` / `RANK_TT_CLIENT_SECRET`）。  
`code` 一次性、约 5 分钟过期，授权后应立即请求。

### `GET /v1/player/profile`

需要 Session（`Authorization: Bearer <session_token>`）。返回当前账号缓存的昵称与头像。

```json
{
  "open_id": "oid",
  "account_id": "tiktok_minis_oid",
  "username": "玩家昵称",
  "nickname": "玩家昵称",
  "avatar_url": "https://...",
  "avatar": "https://...",
  "source": "cache"
}
```

头像为空时客户端可点头像重试 → 调 `POST /v1/player/profile/sync`。

### `POST /v1/player/profile/sync`

需要 Session。服务端向渠道（TikTok Open API / mock）拉取 nickname + avatar 并落库。

```json
{
  "access_token": "<可选，TikTok access_token>",
  "code": "<可选，服务端换票后再拉资料>"
}
```

mock 模式无需 token，会写入 `TT_<open_id>` 与占位头像 URL。

### `POST /v1/player/profile`

需要 Session。客户端把 TTMinisSDK 拿到的昵称/头像推给服务器：

```json
{
  "nickname": "玩家昵称",
  "avatar_url": "https://..."
}
```

### 账号表 `player_register`

注册信息直接落在账号表（首注写入，重复注册不覆盖）：

| 字段 | 说明 |
|------|------|
| app_id / channel / open_id / player_id | 账号维度（`player_id` = `{channel}_{open_id}`） |
| username / avatar_url / click_id | 昵称、头像与推广归因 |
| registered_at_ms | 注册时间 |
| extra_json | 扩展字段（同步含 username/nickname/avatar） |

统计可按 `channel`、`click_id`、`registered_at_ms` 查该表。

### `GET /v1/stats/register?app_id=...`

Service Token；可加 `channel_id` 过滤。响应含 `total`（账号数）与 `by_channel`。

## 客户端（Cocos）

`MiniGameClient.ensureSession()`：

1. `login`
2. 若 `NEED_REGISTER` → 弹窗 username → `register`（自动带 `TTMinisSDK.getTtclid()` / params `clickid`）→ 再 `login`

## gRPC

- `PlayerService.ReportPlayerRegister`：`player_id` 字段传 **open_id**（服务端拼 `channel_openid`）
- `PlayerService.GetRegisterStats`
