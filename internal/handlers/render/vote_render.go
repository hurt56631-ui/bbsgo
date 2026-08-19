package render

import (
	"bbs-go/internal/models"
	"bbs-go/internal/models/resp"
	"bbs-go/internal/pkg/common"
	"bbs-go/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/mlogclub/simple/common/dates"
)

func BuildVote(ctx *gin.Context, vote *models.Vote) *resp.VoteResponse {
	if vote == nil {
		return nil
	}

	currentUserId := common.GetCurrentUserID(ctx)
	var record *models.VoteRecord
	if currentUserId > 0 {
		record = services.VoteRecordService.GetBy(currentUserId, vote.Id)
	}
	return buildVoteWithData(vote, services.VoteOptionService.FindByVoteId(vote.Id), record)
}

// BuildVotes assembles all votes displayed on a topic page with three bounded
// queries: votes, options and the current user's records. This replaces the
// previous three-query-per-topic pattern.
func BuildVotes(ctx *gin.Context, voteIds []int64) map[int64]*resp.VoteResponse {
	result := make(map[int64]*resp.VoteResponse)
	if len(voteIds) == 0 {
		return result
	}

	votes := services.VoteService.GetMap(voteIds)
	optionsByVoteId := services.VoteOptionService.FindByVoteIds(voteIds)
	recordsByVoteId := services.VoteRecordService.FindByUserAndVoteIds(common.GetCurrentUserID(ctx), voteIds)
	for voteId, vote := range votes {
		voteCopy := vote
		var record *models.VoteRecord
		if value, ok := recordsByVoteId[voteId]; ok {
			recordCopy := value
			record = &recordCopy
		}
		result[voteId] = buildVoteWithData(&voteCopy, optionsByVoteId[voteId], record)
	}
	return result
}

func buildVoteWithData(vote *models.Vote, options []models.VoteOption, record *models.VoteRecord) *resp.VoteResponse {
	if vote == nil {
		return nil
	}

	ret := &resp.VoteResponse{
		Id:          vote.Id,
		Type:        vote.Type,
		Title:       vote.Title,
		ExpiredAt:   vote.ExpiredAt,
		VoteNum:     vote.VoteNum,
		OptionCount: vote.OptionCount,
		VoteCount:   vote.VoteCount,
		Expired:     dates.NowTimestamp() > vote.ExpiredAt,
	}

	var selectedMap map[int64]bool
	if record != nil {
		ret.Voted = true
		ret.OptionIds = services.VoteService.ParseOptionIds(record.OptionIds)
		selectedMap = make(map[int64]bool, len(ret.OptionIds))
		for _, optionId := range ret.OptionIds {
			selectedMap[optionId] = true
		}
	}

	for _, option := range options {
		item := resp.VoteOptionResponse{
			Id:        option.Id,
			Content:   option.Content,
			SortNo:    option.SortNo,
			VoteCount: option.VoteCount,
		}
		if vote.VoteCount > 0 {
			item.Percent = float64(option.VoteCount) / float64(vote.VoteCount) * 100
		}
		if selectedMap != nil {
			item.Voted = selectedMap[option.Id]
		}
		ret.Options = append(ret.Options, item)
	}
	return ret
}
