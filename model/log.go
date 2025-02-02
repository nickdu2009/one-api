package model

import (
	"context"
	"fmt"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"one-api/common"
	"sync"
	"time"
)

var asyncWriteConsumeLogMutex *sync.Mutex
var currentConsumeLogQueue []*Log
var nextConsumerLogQueue []*Log

func init() {
	asyncWriteConsumeLogMutex = &sync.Mutex{}
	currentConsumeLogQueue = make([]*Log, 0, 1024)
	nextConsumerLogQueue = make([]*Log, 0, 1024)
}

func InitAsyncWriteConsumeLogWriter(ctx context.Context) {
	go func() {
		for {
			time.Sleep(time.Duration(common.AsyncWriteConsumeLogFrequency) * time.Second)
			asyncWriteConsumeLogWorker(ctx)
		}
	}()
}

func asyncWriteConsumeLogWorker(ctx context.Context) {
	common.SysLog("asyncWriteConsumeLogWorker started")
	asyncWriteConsumeLogMutex.Lock()
	var data []*Log
	if len(currentConsumeLogQueue) > 0 {
		data = currentConsumeLogQueue
		currentConsumeLogQueue = nextConsumerLogQueue
		nextConsumerLogQueue = data[0:0]
	}
	asyncWriteConsumeLogMutex.Unlock()
	common.SysLog(fmt.Sprintf("asyncWriteConsumeLogWorker data len %d", len(data)))
	batchCreateSize := 1024
	for i := 0; i < len(data); i = i + batchCreateSize {
		var updateData []*Log
		if i+batchCreateSize < len(data) {
			updateData = data[i : i+batchCreateSize]
		} else {
			updateData = data[i:len(data)]
		}
		err := DB.WithContext(ctx).Create(updateData).Error
		if err != nil {
			common.SysError(fmt.Sprintf("asyncWriteConsumeLogWorker %+v", err))
		}
	}
	common.SysLog("asyncWriteConsumeLogWorker ended")
}

type Log struct {
	Id               int    `json:"id;index:idx_created_at_id,priority:1"`
	UserId           int    `json:"user_id" gorm:"index"`
	CreatedAt        int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:2;index:idx_created_at_type"`
	Type             int    `json:"type" gorm:"index:idx_created_at_type"`
	Content          string `json:"content"`
	Username         string `json:"username" gorm:"index:index_username_model_name,priority:2;default:''"`
	TokenName        string `json:"token_name" gorm:"index;default:''"`
	ModelName        string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota            int    `json:"quota" gorm:"default:0"`
	PromptTokens     int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens int    `json:"completion_tokens" gorm:"default:0"`
	ChannelId        int    `json:"channel" gorm:"index"`
}

const (
	LogTypeUnknown = iota
	LogTypeTopup
	LogTypeConsume
	LogTypeManage
	LogTypeSystem
)

func RecordLog(ctx context.Context, userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	log := &Log{
		UserId:    userId,
		Username:  GetUsernameById(ctx, userId),
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := DB.WithContext(ctx).Create(log).Error
	if err != nil {
		common.SysError("failed to record log: " + err.Error())
	}
}

func RecordConsumeLog(ctx context.Context, userId int, channelId int, promptTokens int, completionTokens int, modelName string, tokenName string, quota int, content string) {
	tracer := otel.Tracer("one-api/model/log")
	ctx, span := tracer.Start(ctx, "RecordConsumeLog")
	defer span.End()

	span.AddEvent("start log file")
	common.LogInfo(ctx, fmt.Sprintf("record consume log: userId=%d, channelId=%d, promptTokens=%d, completionTokens=%d, modelName=%s, tokenName=%s, quota=%d, content=%s", userId, channelId, promptTokens, completionTokens, modelName, tokenName, quota, content))
	span.AddEvent("end log file")

	if !common.LogConsumeEnabled {
		return
	}
	log := &Log{
		UserId:           userId,
		Username:         GetUsernameById(ctx, userId),
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          content,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            quota,
		ChannelId:        channelId,
	}

	if common.AsyncWriteConsumeLogEnable {
		asyncWriteConsumeLogMutex.Lock()
		currentConsumeLogQueue = append(currentConsumeLogQueue, log)
		asyncWriteConsumeLogMutex.Unlock()
	} else {
		err := DB.WithContext(ctx).Create(log).Error
		if err != nil {
			common.LogError(ctx, "failed to record log: "+err.Error())
		}
	}

}

func GetAllLogs(ctx context.Context, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int) (logs []*Log, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = DB.WithContext(ctx)
	} else {
		tx = DB.WithContext(ctx).Where("type = ?", logType)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
	}
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	return logs, err
}

func GetUserLogs(ctx context.Context, userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int) (logs []*Log, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = DB.WithContext(ctx).Where("user_id = ?", userId)
	} else {
		tx = DB.WithContext(ctx).Where("user_id = ? and type = ?", userId, logType)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Omit("id").Find(&logs).Error
	return logs, err
}

func SearchAllLogs(ctx context.Context, keyword string) (logs []*Log, err error) {
	err = DB.WithContext(ctx).Where("type = ? or content LIKE ?", keyword, keyword+"%").Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	return logs, err
}

func SearchUserLogs(ctx context.Context, userId int, keyword string) (logs []*Log, err error) {
	err = DB.WithContext(ctx).Where("user_id = ? and type = ?", userId, keyword).Order("id desc").Limit(common.MaxRecentItems).Omit("id").Find(&logs).Error
	return logs, err
}

func SumUsedQuota(ctx context.Context, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int) (quota int) {
	tx := DB.WithContext(ctx).Table("logs").Select("ifnull(sum(quota),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&quota)
	return quota
}

func SumUsedToken(ctx context.Context, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := DB.WithContext(ctx).Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	result := DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Delete(&Log{})
	return result.RowsAffected, result.Error
}
