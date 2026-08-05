package grpcapi

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	rankv1 "minigameserver/api/gen/rank/v1"
	"minigameserver/internal/domain"
	"minigameserver/internal/service"
)

type RankServer struct {
	rankv1.UnimplementedRankServiceServer
	svc *service.Service
}

type PlayerServer struct {
	rankv1.UnimplementedPlayerServiceServer
	svc *service.Service
}

func Register(gs *grpc.Server, svc *service.Service) {
	rankv1.RegisterRankServiceServer(gs, &RankServer{svc: svc})
	rankv1.RegisterPlayerServiceServer(gs, &PlayerServer{svc: svc})
}

func (s *RankServer) UpsertMaxScore(ctx context.Context, req *rankv1.UpsertMaxScoreRequest) (*rankv1.UpsertMaxScoreResponse, error) {
	if err := requireServiceAuth(ctx, s.svc); err != nil {
		return nil, err
	}
	var writeSpec domain.PeriodType
	if rt := strings.TrimSpace(req.GetRankType()); rt != "" {
		writeSpec = domain.PeriodType(rt)
	} else {
		pt, err := protoPeriod(req.GetPeriodType())
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		writeSpec = pt
	}
	out, err := s.svc.UpsertMaxScore(ctx, service.UpsertInput{
		AppID: req.GetAppId(), BoardID: req.GetBoardId(), ZoneID: req.GetZoneId(),
		Channel: req.GetChannel(), PlayerID: req.GetPlayerId(), Score: req.GetScore(), PeriodType: writeSpec,
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp := &rankv1.UpsertMaxScoreResponse{Channel: out.Channel}
	if out.Week != nil {
		resp.Week = &rankv1.PeriodScoreResult{Updated: out.Week.Updated, BestScore: out.Week.BestScore, SelfRank: out.Week.SelfRank}
	}
	if out.Month != nil {
		resp.Month = &rankv1.PeriodScoreResult{Updated: out.Month.Updated, BestScore: out.Month.BestScore, SelfRank: out.Month.SelfRank}
	}
	if out.Day != nil {
		resp.Day = &rankv1.PeriodScoreResult{Updated: out.Day.Updated, BestScore: out.Day.BestScore, SelfRank: out.Day.SelfRank}
	}
	if out.All != nil {
		resp.All = &rankv1.PeriodScoreResult{Updated: out.All.Updated, BestScore: out.All.BestScore, SelfRank: out.All.SelfRank}
	}
	return resp, nil
}

func (s *RankServer) GetLeaderboard(ctx context.Context, req *rankv1.GetLeaderboardRequest) (*rankv1.GetLeaderboardResponse, error) {
	if err := requireServiceAuth(ctx, s.svc); err != nil {
		return nil, err
	}
	var pt domain.PeriodType
	var err error
	if rt := strings.TrimSpace(req.GetRankType()); rt != "" {
		pt, err = domain.ParseRankType(rt)
	} else {
		pt, err = protoPeriodQuery(req.GetPeriodType())
	}
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	out, err := s.svc.GetLeaderboard(ctx, service.LeaderboardInput{
		AppID: req.GetAppId(), BoardID: req.GetBoardId(), ZoneID: req.GetZoneId(),
		Channel: req.GetChannel(), PeriodType: pt, TopN: int(req.GetTopN()), PlayerID: req.GetPlayerId(),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &rankv1.GetLeaderboardResponse{
		SnapshotTsMs: out.SnapshotTs,
		CacheHit:     out.CacheHit,
		SelfRank:     out.SelfRank,
		SelfScore:    out.SelfScore,
		EntriesGzip:  out.EntriesRaw,
		Channel:      out.Channel,
		RankType:     out.RankType,
	}, nil
}

func (s *RankServer) SetImRankData(ctx context.Context, req *rankv1.SetImRankDataRequest) (*rankv1.SetImRankDataResponse, error) {
	if err := requireServiceAuth(ctx, s.svc); err != nil {
		return nil, err
	}
	out, err := s.svc.SetImRankData(ctx, service.SetImRankDataInput{
		AppID: req.GetAppId(), BoardID: req.GetBoardId(), ZoneID: req.GetZoneId(),
		Channel: req.GetChannel(), PlayerID: req.GetPlayerId(), DataType: req.GetDataType(),
		Value: req.GetValue(), Priority: req.GetPriority(), Extra: req.GetExtra(),
		RankType: req.GetRankType(), RelationType: req.GetRelationType(),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp := &rankv1.SetImRankDataResponse{Channel: out.Channel}
	if out.Day != nil {
		resp.Day = &rankv1.PeriodScoreResult{Updated: out.Day.Updated, BestScore: out.Day.BestScore, SelfRank: out.Day.SelfRank}
	}
	if out.Week != nil {
		resp.Week = &rankv1.PeriodScoreResult{Updated: out.Week.Updated, BestScore: out.Week.BestScore, SelfRank: out.Week.SelfRank}
	}
	if out.Month != nil {
		resp.Month = &rankv1.PeriodScoreResult{Updated: out.Month.Updated, BestScore: out.Month.BestScore, SelfRank: out.Month.SelfRank}
	}
	if out.All != nil {
		resp.All = &rankv1.PeriodScoreResult{Updated: out.All.Updated, BestScore: out.All.BestScore, SelfRank: out.All.SelfRank}
	}
	return resp, nil
}

func (s *RankServer) GetImRankList(ctx context.Context, req *rankv1.GetImRankListRequest) (*rankv1.GetImRankListResponse, error) {
	if err := requireServiceAuth(ctx, s.svc); err != nil {
		return nil, err
	}
	out, err := s.svc.GetImRankList(ctx, service.GetImRankListInput{
		AppID: req.GetAppId(), BoardID: req.GetBoardId(), ZoneID: req.GetZoneId(),
		Channel: req.GetChannel(), PlayerID: req.GetPlayerId(), RelationType: req.GetRelationType(),
		RankType: req.GetRankType(), DataType: req.GetDataType(), Suffix: req.GetSuffix(),
		RankTitle: req.GetRankTitle(), TopN: int(req.GetTopN()),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp := &rankv1.GetImRankListResponse{
		RankType: out.RankType, RelationType: out.RelationType, DataType: out.DataType,
		ZoneId: out.ZoneID, Suffix: out.Suffix, RankTitle: out.RankTitle, Display: out.Display,
		FriendUnsupported: out.FriendUnsupported, SnapshotTsMs: out.SnapshotTs, CacheHit: out.CacheHit,
		SelfRank: out.SelfRank, SelfScore: out.SelfScore, EntriesGzip: out.EntriesRaw,
		Channel: out.Channel,
	}
	for _, it := range out.Items {
		resp.Items = append(resp.Items, &rankv1.CompactRankEntry{R: it.R, P: it.P, S: it.S})
	}
	return resp, nil
}

func (s *RankServer) GetImRankData(ctx context.Context, req *rankv1.GetImRankDataRequest) (*rankv1.GetImRankDataResponse, error) {
	if err := requireServiceAuth(ctx, s.svc); err != nil {
		return nil, err
	}
	out, err := s.svc.GetImRankData(ctx, service.GetImRankDataInput{
		AppID: req.GetAppId(), BoardID: req.GetBoardId(), ZoneID: req.GetZoneId(),
		Channel: req.GetChannel(), PlayerID: req.GetPlayerId(), RelationType: req.GetRelationType(),
		RankType: req.GetRankType(), DataType: req.GetDataType(),
		PageNum: int(req.GetPageNum()), PageSize: int(req.GetPageSize()),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp := &rankv1.GetImRankDataResponse{
		RankType: out.RankType, RelationType: out.RelationType, DataType: out.DataType,
		ZoneId: out.ZoneID, PageNum: int32(out.PageNum), PageSize: int32(out.PageSize),
		FriendUnsupported: out.FriendUnsupported, SnapshotTsMs: out.SnapshotTs, CacheHit: out.CacheHit,
		SelfRank: out.SelfRank, SelfScore: out.SelfScore, Total: int32(out.Total),
		Channel: out.Channel,
	}
	for _, it := range out.Items {
		resp.Items = append(resp.Items, &rankv1.CompactRankEntry{R: it.R, P: it.P, S: it.S})
	}
	return resp, nil
}

func (s *PlayerServer) ReportPlayerRegister(ctx context.Context, req *rankv1.ReportPlayerRegisterRequest) (*rankv1.ReportPlayerRegisterResponse, error) {
	if err := requireServiceAuth(ctx, s.svc); err != nil {
		return nil, err
	}
	out, err := s.svc.Register(ctx, service.RegisterInput{
		AppID: req.GetAppId(), Channel: req.GetChannel(), OpenID: req.GetPlayerId(),
		PlatformKind: req.GetPlatformKind(), ExtraJSON: req.GetExtraJson(),
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	return &rankv1.ReportPlayerRegisterResponse{
		IsNew: out.IsNew, Channel: out.Channel, RegisteredAtMs: out.RegisteredAtMs,
	}, nil
}

func (s *PlayerServer) GetRegisterStats(ctx context.Context, req *rankv1.GetRegisterStatsRequest) (*rankv1.GetRegisterStatsResponse, error) {
	if err := requireServiceAuth(ctx, s.svc); err != nil {
		return nil, err
	}
	out, err := s.svc.GetRegisterStats(ctx, req.GetAppId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	resp := &rankv1.GetRegisterStatsResponse{Total: out.Total}
	for _, c := range out.ByChannel {
		resp.ByChannel = append(resp.ByChannel, &rankv1.ChannelCount{Channel: c.Channel, Count: c.Count})
	}
	return resp, nil
}

func requireServiceAuth(ctx context.Context, svc *service.Service) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	token := firstMeta(md, "authorization")
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		token = firstMeta(md, "x-rank-service-token")
	}
	if !svc.CheckServiceToken(token) {
		return status.Error(codes.Unauthenticated, "invalid service token")
	}
	return nil
}

func firstMeta(md metadata.MD, key string) string {
	vals := md.Get(key)
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func protoPeriod(p rankv1.PeriodType) (domain.PeriodType, error) {
	switch p {
	case rankv1.PeriodType_PERIOD_UNSPECIFIED, rankv1.PeriodType_PERIOD_BOTH:
		return domain.PeriodBoth, nil
	case rankv1.PeriodType_PERIOD_WEEK:
		return domain.PeriodWeek, nil
	case rankv1.PeriodType_PERIOD_MONTH:
		return domain.PeriodMonth, nil
	case rankv1.PeriodType_PERIOD_DAY:
		return domain.PeriodDay, nil
	case rankv1.PeriodType_PERIOD_ALL:
		return domain.PeriodAll, nil
	default:
		return "", fmt.Errorf("invalid period_type")
	}
}

func protoPeriodQuery(p rankv1.PeriodType) (domain.PeriodType, error) {
	switch p {
	case rankv1.PeriodType_PERIOD_WEEK:
		return domain.PeriodWeek, nil
	case rankv1.PeriodType_PERIOD_MONTH:
		return domain.PeriodMonth, nil
	case rankv1.PeriodType_PERIOD_DAY:
		return domain.PeriodDay, nil
	case rankv1.PeriodType_PERIOD_ALL:
		return domain.PeriodAll, nil
	default:
		return "", fmt.Errorf("period_type must be DAY, WEEK, MONTH, or ALL")
	}
}
