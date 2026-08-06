# ??????????

?? `docs/design.md` / `docs/decisions.md` / `docs/register.md`?????`go test ./...`

| ID | ?? | ?? | ?? |
|---|---|---|---|
| A1 | ???? | `memory_test.TestUpsertMaxOnlyIncrease` + HTTP acceptance | ??? |
| A2 | ?????? | `memory_test.TestTopNTieBreakFirstWins` | ??? |
| A3 | board_key ? channel ?? | `domain_test.TestBoardKeyStableAndChannelIsolated` | ??? |
| A4 | ?/? ISO ??UTC? | `domain_test.TestPeriodKeyWeekMonthUTC` | ??? |
| A5 | Session ??/?? | `auth_test.TestSessionIssueParse` | ??? |
| A6 | HTTP session + ???? + gzip ?? + cache_hit | `acceptance.TestHTTP_SessionScoreLeaderboard_AgainstDesign` | ??? |
| A7 | ? session ?? 401 | `acceptance.TestHTTP_UnauthorizedWithoutSession` | ??? |
| A8 | gRPC ?????/? + entries_gzip | `acceptance.TestGRPC_UpsertAndLeaderboard_ServiceAuth` | ??? |
| A9 | Redis stub ?? | `internal/store/redis` | ???? |
| A10 | MySQL schema | `deployments/db/schema.sql` | compose ?? |
| A11 | ?????? + HTTP ?? | `acceptance.TestHTTP_PlayerRegister_FirstTouchChannel` | ??? |
| A12 | gRPC ??/?? | `acceptance.TestGRPC_PlayerRegisterAndStats` | ??? |
| A13 | ImRank set/list/data + reject relationType | `acceptance.TestHTTP_ImRankAlignedAPIs` | ??? |
| A14 | Login NEED_REGISTER then register+login | `acceptance.TestHTTP_LoginNeedRegisterThenLogin` | ??? |

## ??

```bash
cd D:\0_mycompany\0projs\MiniGameServer
go test ./...
go run ./cmd/minigameserver
```
