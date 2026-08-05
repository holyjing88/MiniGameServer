# 技术设计复核记录（2026-07-31）

对 `design.md` / `decisions.md` 做一致性与可实现性复核。结论：**可进入 M1**，但建议先确认 `open-questions.md` 剩余项（可用默认值）。

---

## 已修复的问题（文档内已改）

| 问题 | 处理 |
|---|---|
| 目录仍写 gRPC / `.proto` 却又写「不做 gRPC」 | **纠正为双栈**：HTTP 给游戏，gRPC 给服务间 |
| `period_key` 示例仍含日榜/总榜 | 仅保留周 `YYYY-Www`、月 `YYYY-MM` |
| 写分只带一个 `period_type` 易漏榜 | 约定默认 **一次请求双写周+月** |
| 裸 `Content-Encoding: gzip` 与 `self_rank` 难共存、宿主行为不一 | 改为 **信封 JSON + entries 的 gzip/base64** |
| `board_key` 未含渠道 | 纳入 `channel`，避免串榜 |
| HotCache 仍提 CompressAlgo/zstd | 统一 gzip |
| 只升不降与同分次序 | 明确：`>` 才更新；同分 `updated_at ASC` 先达优先 |
| `Upsert` 命名易误解为覆盖 | 接口改为 `UpsertMax` |

---

## 仍存在的风险 / 假设

1. **分数可信**：任何持有合法 session 的客户端可上报任意高分；本期只靠限流，不做服务端玩法校验。  
2. **TikTok 换票**：`/v1/session` 依赖 TikTok OAuth；需配置 `client_key/secret`，网络与合规要通。  
3. **单机缓存**：多开实例会出现榜短暂不一致；已接受，上 Redis 前保持单实例。  
4. **ISO 周 + 时区**：未确认 R3 前，实现用可配置时区（默认 UTC）。  
5. **open_id 较长**：Top100 gzip 后通常仍可接受；若不够再做短 id。

---

## 与决策表对照

| 决策 | 设计是否一致 |
|---|---|
| Session Token | 是（HTTP `/v1/session` + Bearer） |
| 游戏客户端 HTTP/HTTPS | 是 |
| 服务间 gRPC | 是（§4.6，与 HTTP 共享 domain） |
| open_id | 是 |
| gzip | 是（entries） |
| 只升不降 | 是（`UpsertMax`） |
| 仅周/月 | 是 |
| 单机无 Redis | 是 |

---

## 建议编码前用默认值锁定的项

若产品暂不回复，M1 采用：

| 项 | 默认 |
|---|---|
| Session 签发 | MiniGameServer 自换 TikTok code |
| 未登录 | 写/拉均需 session |
| 时区 | UTC |
| TopN | 100，无昵称 |
| 旧榜 | 服务先独立 |
