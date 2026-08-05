# 已拍板决策（2026-07-31）

| # | 决策 |
|---|---|
| 鉴权 | **Session Token**（登录后短期 token，写分/拉榜携带） |
| 传输（游戏客户端） | **HTTP/HTTPS**（Cocos 2.4 不接 gRPC-Web） |
| 传输（服务间） | **保留 gRPC**（内部服务 / 其它后端调 MiniGameServer） |
| 玩家 ID | **`account_id = channel_id + "_" + open_id`** |
| 登录 | 未注册返回 `NEED_REGISTER`，客户端注册后再登录 |
| 压缩 | **gzip**（拉榜 entries；信封见 design §4.3） |
| 写分 | **只升不降**（只记最高分） |
| 周期 | **day / week / month / all**（`rankType`）；写分默认 full 四窗或 both |
| 渠道 | **所有接口必带 `channel` / `channel_id`**，进 board_key，跨平台分榜 |
| 部署 | **单机**内存热点；**不上 Redis** |
| 与旧 ChallengeHttp | **服务先独立**，游戏对接另排期 |

说明与剩余问题：[open-questions.md](open-questions.md)  
复核记录：[review-notes.md](review-notes.md)
