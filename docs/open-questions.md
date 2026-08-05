# 待确认问题（剩余）

已拍板见 [decisions.md](decisions.md)。不回复则按 **默认** 进 M1（见 [review-notes.md](review-notes.md)）。

---

## R1. Session 谁签发？

- [ ] **A.** MiniGameServer：`POST /v1/session` 收 TikTok `code`，服务端换 `open_id` 后签发  
- [ ] **B.** 已有账号服发 token，MiniGameServer 只验签  

**默认：A**

---

## R2. 未登录

- [ ] 写分、拉榜都要 session  
- [ ] 写分要 session；拉榜允许游客看 TopN（无 self_rank）  

**默认：写/拉都要 session**

---

## R3. 周/月切桶时区

- [ ] UTC  
- [ ] UTC+8  

周格式：ISO-8601（`YYYY-Www`）。  

**默认：UTC**

---

## R4. TopN / 昵称

- [ ] TopN = 50 / **100** / 200  
- [ ] 拉榜不带昵称头像（已与最小包体一致）  

**默认：Top100；不带昵称**

---

## R5. 与旧 ChallengeHttp

- [ ] A 替换 / B 双轨 / **C 服务先独立**  

**默认：C**
