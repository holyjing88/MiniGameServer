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

登录成功响应含 `username`、`click_id`；客户端右上角展示 `openid` + `用户名`。

### 账号表 `player_register`

注册信息直接落在账号表（首注写入，重复注册不覆盖）：

| 字段 | 说明 |
|------|------|
| app_id / channel / open_id / player_id | 账号维度（`player_id` = `{channel}_{open_id}`） |
| username / click_id | 昵称与推广归因 |
| registered_at_ms | 注册时间 |
| extra_json | 扩展字段（同步含 username/click_id） |

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
