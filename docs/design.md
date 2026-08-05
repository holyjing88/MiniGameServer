# MiniGameServer 设计文档

> 原名 RankServer；新增玩家注册渠道上报，见 [register.md](register.md)。

> 状态：技术复核修订版（2026-07-31）+ 双栈通信  
> 决策来源：[decisions.md](decisions.md)  
> 游戏客户端：Cocos Creator **2.4.12** → **HTTP/HTTPS**  
> 服务间：其它后端 / 内部服务 → **gRPC**  
> 首批渠道：无平台 ImRank（如 TikTok Minis）→ `NO_NATIVE_RANK`

---

## 1. 目标与边界

### 1.1 要解决什么

平台**没有**托管排行 API 时，需要：

1. 写入玩家最高成绩（只升不降）
2. 拉取 **周榜 / 月榜** TopN，并尽量返回自己的名次
3. 控制下行体积（短字段 JSON + **gzip**）

### 1.2 平台类型

| PlatformKind | 含义 | 本服务 |
|---|---|---|
| `NO_NATIVE_RANK` (1) | 无平台排行 API | **主路径** |
| `NATIVE_RANK` (2) | 有平台排行（如抖音 ImRank） | 本期不接；客户端走平台 |

渠道用 `channel` 区分（如 `tiktok_minis`），**不为每渠道复制一套 API**。

### 1.3 本期非目标

- 好友榜 / 关系链
- 完整反作弊（录像、设备指纹等）——客户端 score 默认可信，仅 Session + 限流
- 管理后台
- Redis / 多实例共享缓存
- 日榜、总榜
- 对 Cocos 暴露 gRPC / gRPC-Web（游戏只走 HTTP）

---

## 2. 架构

```
┌──────────────┐  HTTPS + Session        ┌──────────────────────────────┐
│ Cocos Client │ ──────────────────────► │ MiniGameServer (Go, 单机)         │
│  2.4.12      │ ◄────────────────────── │                              │
└──────────────┘  gzip leaderboard       │  api/http  ← 游戏客户端       │
                                         │  api/grpc  ← 服务间调用       │
┌──────────────┐  gRPC (内网/mTLS 可选)   │         │                    │
│ Other Backend│ ──────────────────────► │         ▼                    │
│ Account/BMS… │ ◄────────────────────── │  auth / domain / hotcache    │
└──────────────┘  RankService            │  store.RankStore             │
                                         │    ├─ mysql (本期)           │
                                         │    └─ redis (接口预留)       │
                                         └──────────────┬───────────────┘
                                                        ▼
                                                    MySQL
```

**同一套领域逻辑**：HTTP 与 gRPC 都调用 `domain` + `store` + `hotcache`，禁止两套写分语义。

| 调用方 | 协议 | 鉴权 |
|---|---|---|
| Cocos 游戏 | HTTP/HTTPS | Session Token（玩家） |
| 其它微服务 | gRPC | 服务凭证（见 §4.6：内网 + metadata token / mTLS） |

### 2.1 模块

| 模块 | 职责 |
|---|---|
| `cmd/minigameserver` | 同时监听 HTTP 端口与 gRPC 端口 |
| `internal/api/http` | 游戏侧 REST；Session；gzip 信封 |
| `internal/api/grpc` | 服务间 `RankService`；小包体 protobuf |
| `api/proto/rank/v1` | `.proto` 定义与生成代码 |
| `internal/auth` | 玩家 session；服务间 credential |
| `internal/domain` | `board_key`、周期桶、只升不降 |
| `internal/store` | `RankStore`；mysql；redis stub |
| `internal/hotcache` | 内存 **gzip** TopN 快照 |
| `internal/config` | 双端口、DB、TTL、TopN、时区 |

---

## 3. 领域模型

### 3.1 榜维度

```
board_key = SHA256(app_id | board_id | zone_id | period_key | channel)[:16]
```

| 字段 | 说明 | 示例 |
|---|---|---|
| `app_id` | 产品 | `parking_smart_brain` |
| `board_id` | 榜类型 | `clear_level` |
| `zone_id` | 环境隔离 | `default` / `test` |
| `period_key` | 周期桶 | 周：`2026-W31`；月：`2026-07` |
| `channel` | 渠道 | `tiktok_minis` |
| `platform_kind` | 能力类 | 写入表字段；**不进 board_key**（同 channel 已隔离） |

> **渠道是否分榜：** `channel` 进入 `board_key`，TikTok 与其它渠道分数互不串榜。若以后要「全渠道大一统」，再去掉 channel 维度。

**排序：** `score DESC`，同分 `updated_at ASC`（先达到该分者靠前）。只升不降时，未刷新最高分则不改 `updated_at`，保序稳定。

### 3.2 周期桶（仅周 / 月）

| `period_type` | `period_key` 规则 | 备注 |
|---|---|---|
| `week` | `YYYY-Www`（**ISO-8601 周**） | 时区见待确认 R3，默认实现先按配置时区切 |
| `month` | `YYYY-MM` | 同上 |

不做：`day`、`all`。

### 3.3 写分语义（只升不降）

对同一 `(board_key, player_id)`：

| 条件 | 行为 |
|---|---|
| 无记录 | 插入 |
| `new_score > old_score` | 更新 score + updated_at |
| `new_score <= old_score` | **不更新**，返回当前最高分 |

### 3.4 一次通关写两榜（推荐）

客户端**不要**为周/月各打一次（易漏）。约定：

`POST /v1/score` **不传** `period_type`（或传 `both`）时，服务端对 **当前周桶 + 当前月桶各做一次**只升不降 Upsert。

若显式传 `week` / `month`，则只更新该周期（调试用）。

### 3.5 玩家条目

| 字段 | 说明 |
|---|---|
| `player_id` | = TikTok **`open_id`**（来自 session，不信任 body 自报） |
| `score` | int64，如通关关卡号 |
| `updated_at` | 最高分写入时间 unix ms |
| `extra` | 可选，≤256B；拉榜默认不返回 |

拉榜条目：**rank + player_id + score**，不带昵称/头像（最小包体）。

---

## 4. HTTP API

Base：`https://<host>/v1`  
鉴权：`Authorization: Bearer <session_token>`（除换票接口外）

### 4.1 `POST /v1/session` — 换 Session

**请求（倾向：MiniGameServer 自签，见 R1）：**

```json
{
  "app_id": "parking_smart_brain",
  "channel": "tiktok_minis",
  "code": "<TTMinis.game.login 返回的 code>"
}
```

**响应：**

```json
{
  "session_token": "...",
  "expires_in": 86400,
  "player_id": "<open_id>"
}
```

服务端用 `code` 调 TikTok OAuth 换 `open_id`，再签发 HMAC/JWT session（含 `app_id, channel, open_id, exp`）。  
**client_secret 只放服务端。**

### 4.2 `POST /v1/score` — 写最高分

```json
{
  "app_id": "parking_smart_brain",
  "board_id": "clear_level",
  "zone_id": "default",
  "channel": "tiktok_minis",
  "score": 128
}
```

可选：`"period_type": "week"|"month"`；默认双写周+月。

**响应：**

```json
{
  "week":  { "updated": true,  "best_score": 128, "self_rank": 42 },
  "month": { "updated": false, "best_score": 200, "self_rank": 15 }
}
```

`self_rank`：0 表示未计算/未知（实现可先省略，M2 再补）。

### 4.3 `GET /v1/leaderboard` — 拉榜

```
GET /v1/leaderboard?app_id=...&board_id=clear_level&zone_id=default&channel=tiktok_minis&period_type=week&top_n=100
```

**响应信封（外层未压缩 JSON，便于带元数据）：**

```json
{
  "snapshot_ts": 1730000000000,
  "cache_hit": true,
  "self_rank": 42,
  "self_score": 128,
  "encoding": "gzip+base64",
  "entries": "<base64(gzip(compact_json_array))>"
}
```

解压后 `entries`：

```json
[{"r":1,"p":"openid_a","s":300},{"r":2,"p":"openid_b","s":280}]
```

| 字段 | 含义 |
|---|---|
| `r` | rank |
| `p` | player_id (open_id) |
| `s` | score |

**为何不用裸 `Content-Encoding: gzip` 整包？**

- 方便同时返回 `self_rank` / `snapshot_ts`
- TikTok/部分宿主对 XHR 自动解压行为不一致；**业务层 gzip + base64** 行为可控  
- HotCache 直接缓存 `entries` 的 gzip 字节，组信封时再 base64

若实测宿主对 `Content-Encoding: gzip` 可靠，可改为：信封 JSON 不大时不压；仅 `entries` 用独立二进制流。以实现时压测为准，**默认采用信封 + gzip(base64)**。

`top_n`：服务端钳制上限（默认 max 100，待 R4 确认）。

### 4.4 错误约定

| HTTP | 含义 |
|---|---|
| 400 | 参数非法 |
| 401 | session 无效/过期 |
| 403 | app/channel 未授权 |
| 429 | 限流 |
| 500 | 内部错误 |

Body：`{"code":"SESSION_EXPIRED","msg":"..."}`

### 4.5 限流（M3）

- 按 `open_id`：写分 ≤ N 次/分钟；拉榜 ≤ M 次/分钟  
- 按 IP：兜底

### 4.6 gRPC（服务间，保留）

进程另开 **gRPC 端口**（如 `:9090`），供 Account / BMS / 其它后端调用。  
语义与 HTTP 对齐，共享 `domain` / `store` / `hotcache`。

**鉴权（与玩家 Session 分离）：**

| 方式 | 说明 |
|---|---|
| 推荐 | metadata `authorization: Bearer <service_token>`（配置下发的服务密钥或内部 JWT） |
| 可选 | 内网 + mTLS；公网禁止裸暴露 gRPC |

**Proto 草案（`api/proto/rank/v1/rank.proto`）：**

```protobuf
syntax = "proto3";
package rank.v1;
option go_package = "minigameserver/api/gen/rank/v1;rankv1";

enum PeriodType {
  PERIOD_UNSPECIFIED = 0;
  PERIOD_WEEK = 1;
  PERIOD_MONTH = 2;
  PERIOD_BOTH = 3; // 写分默认：周+月
}

service RankService {
  // 服务端已持有 open_id 时直接写分（不再走 TikTok code）
  rpc UpsertMaxScore(UpsertMaxScoreRequest) returns (UpsertMaxScoreResponse);
  rpc GetLeaderboard(GetLeaderboardRequest) returns (GetLeaderboardResponse);
  // 可选：内部代换 session，一般不需要
}

message UpsertMaxScoreRequest {
  string app_id = 1;
  string board_id = 2;
  string zone_id = 3;
  string channel = 4;
  string player_id = 5;     // open_id
  int64 score = 6;
  PeriodType period_type = 7; // 默认 BOTH
}

message PeriodScoreResult {
  bool updated = 1;
  int64 best_score = 2;
  int32 self_rank = 3;
}

message UpsertMaxScoreResponse {
  PeriodScoreResult week = 1;
  PeriodScoreResult month = 2;
}

message GetLeaderboardRequest {
  string app_id = 1;
  string board_id = 2;
  string zone_id = 3;
  string channel = 4;
  PeriodType period_type = 5; // WEEK 或 MONTH
  int32 top_n = 6;
  string player_id = 7;       // 可选，算 self_rank
}

message GetLeaderboardResponse {
  int64 snapshot_ts_ms = 1;
  bool cache_hit = 2;
  int32 self_rank = 3;
  int64 self_score = 4;
  // 已 gzip 的紧凑条目；服务间可再开 grpc gzip compressor
  bytes entries_gzip = 5;
}
```

**与 HTTP 差异：**

| | HTTP（游戏） | gRPC（服务间） |
|---|---|---|
| 换 TikTok session | 有 `/v1/session` | 无（调用方已有 open_id） |
| 写分玩家身份 | 来自 session | 请求里显式 `player_id` + 服务凭证 |
| 拉榜压缩 | 信封 + base64(gzip) | `bytes entries_gzip`（可直接二进制） |

---

## 5. 存储

### 5.1 `RankStore`（预留 Redis）

```go
type Entry struct {
    BoardKey  []byte
    PlayerID  string
    Score     int64
    UpdatedAt int64
    Extra     []byte
}

type RankStore interface {
    // 只升不降；updated=是否刷新了最高分；best=当前库中最高分
    UpsertMax(ctx context.Context, e Entry) (updated bool, best int64, err error)
    TopN(ctx context.Context, boardKey []byte, n int) ([]Entry, error)
    RankOf(ctx context.Context, boardKey []byte, playerID string) (rank int32, score int64, ok bool, err error)
}
```

- 本期：`MySQLStore`
- 预留：`RedisStore`（`ZSET`，score 为成绩；member 为 player_id）——**不实现**，仅留空文件/接口注释

### 5.2 MySQL

```sql
CREATE TABLE rank_score (
  id            BIGINT PRIMARY KEY AUTO_INCREMENT,
  board_key     BINARY(16)     NOT NULL,
  app_id        VARCHAR(64)    NOT NULL,
  board_id      VARCHAR(64)    NOT NULL,
  zone_id       VARCHAR(32)    NOT NULL DEFAULT 'default',
  period_key    VARCHAR(16)    NOT NULL,
  platform_kind TINYINT        NOT NULL DEFAULT 1,
  channel       VARCHAR(32)    NOT NULL DEFAULT '',
  player_id     VARCHAR(128)   NOT NULL,
  score         BIGINT         NOT NULL,
  extra         VARBINARY(256) NULL,
  updated_at    BIGINT         NOT NULL,
  UNIQUE KEY uk_board_player (board_key, player_id),
  KEY idx_board_score (board_key, score DESC, updated_at ASC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE rank_board_meta (
  app_id      VARCHAR(64) NOT NULL,
  board_id    VARCHAR(64) NOT NULL,
  zone_id     VARCHAR(32) NOT NULL DEFAULT 'default',
  top_n       INT         NOT NULL DEFAULT 100,
  refresh_sec INT         NOT NULL DEFAULT 30,
  PRIMARY KEY (app_id, board_id, zone_id)
) ENGINE=InnoDB;

-- 可选：session 落库（也可用纯内存 + HMAC JWT 无状态）
CREATE TABLE rank_session (
  token_hash  BINARY(32)  NOT NULL PRIMARY KEY,
  player_id   VARCHAR(128) NOT NULL,
  app_id      VARCHAR(64)  NOT NULL,
  channel     VARCHAR(32)  NOT NULL,
  expires_at  BIGINT       NOT NULL,
  KEY idx_exp (expires_at)
) ENGINE=InnoDB;
```

Upsert SQL 语义：

```sql
INSERT INTO rank_score (...) VALUES (...)
ON DUPLICATE KEY UPDATE
  score = IF(VALUES(score) > score, VALUES(score), score),
  updated_at = IF(VALUES(score) > score, VALUES(updated_at), updated_at),
  extra = IF(VALUES(score) > score, VALUES(extra), extra);
```

**名次查询（示意）：**

```sql
SELECT 1 + COUNT(*) FROM rank_score
WHERE board_key = ? AND (score > ? OR (score = ? AND updated_at < ?));
```

---

## 6. 热点缓存

### 6.1 结构

```
HotCache: map[board_key_hex] → HotItem {
  entriesGzip []byte   // gzip(compact JSON array)
  snapshotTs  int64
  topScoreMin int64    // TopN 最后一名分数，写路径 dirty 判断用
}
```

单机进程内 map + RWMutex；**不跨实例共享**（已拍板）。

### 6.2 刷新策略

| 触发 | 行为 |
|---|---|
| 定时 | 每 `refresh_sec`（默认 30s）重建「近期有写或被拉过」的 board |
| 写分 | 若 `score >= topScoreMin`（可能进榜）→ dirty；debounce 1～2s 重建该 board |
| 冷启动 | 预热配置中的 board 列表 |

拉榜：优先返回 HotItem；miss 则查 DB → 构建 → 回填。  
`self_rank`：若在 TopN 快照内直接取；否则 `RankOf` 查 MySQL（可接受）。

### 6.3 周/月切换

新 `period_key` 自然产生新 `board_key` → 新缓存条目；旧桶可保留历史，按需 TTL 淘汰冷 key（后期）。

---

## 7. Session 设计要点

| 项 | 建议（待 R1 确认） |
|---|---|
| 签发 | MiniGameServer：`code` → TikTok token API → `open_id` → 签 JWT/HMAC |
| 载荷 | `app_id, channel, player_id, exp` |
| TTL | 24h（可配置）；客户端过期重走 `/v1/session` |
| 存储 | **无状态 JWT** 优先（单机无 Redis）；或 token_hash 进 `rank_session` 便于吊销 |
| 传输 | 仅 HTTPS；禁止 URL query 带 token |

---

## 8. 安全与风险（复核）

| 风险 | 本期态度 |
|---|---|
| 客户端伪造高分 | 接受；依赖 session 归属 + 限流；反作弊后期 |
| Session 被盗 | HTTPS + 短 TTL |
| TikTok `code` 换票失败 | 401/502；客户端重登 |
| 包体仍偏大（open_id 长） | 可接受；后期可对 TopN 内 open_id 做短 id 字典 |

---

## 9. 工程目录（落地）

```
MiniGameServer/
  README.md
  docs/
  api/proto/rank/v1/rank.proto
  api/gen/                 # protoc / buf 生成
  cmd/minigameserver/main.go   # 同时 Listen HTTP + gRPC
  internal/
    config/
    api/http/
    api/grpc/
    auth/
    domain/
    store/
      store.go
      mysql/
      redis/               # stub
    hotcache/
  deployments/
    docker-compose.yml
    schema.sql
  go.mod
```

---

## 10. 客户端衔接（ParkingSmartBrainEx）

| 时机 | 调用 |
|---|---|
| 启动登录成功 | HTTP `POST /v1/session` |
| 主线通关 | HTTP `POST /v1/score` |
| 点排行榜 | HTTP `GET /v1/leaderboard` |
| 其它后端需要写/拉榜 | **gRPC `RankService`** |
| 抖音 ImRank 可用 | 不走本服务 |
| 旧 ChallengeHttp | 服务先独立 |

Cocos：只使用 HTTP；不解、不链 gRPC。

---

## 11. 里程碑

| 阶段 | 交付 |
|---|---|
| M0 | 文档与决策 |
| M1 | HTTP + MySQL + Session + 写/拉；**gRPC RankService 同步暴露** |
| M2 | HotCache + gzip entries |
| M3 | 限流 + docker-compose；服务间 token/mTLS |
| M4 | 游戏 TikTok 包对接（HTTP） |
| M5 | RedisStore（多实例时） |
