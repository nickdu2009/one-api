package model

import (
	"context"
	"gorm.io/gorm"
	"one-api/common"
)

type Channel struct {
	Id                 int     `json:"id"`
	Type               int     `json:"type" gorm:"default:0"`
	Key                string  `json:"key" gorm:"not null;index"`
	Status             int     `json:"status" gorm:"default:1"`
	Name               string  `json:"name" gorm:"index"`
	Weight             *uint   `json:"weight" gorm:"default:0"`
	CreatedTime        int64   `json:"created_time" gorm:"bigint"`
	TestTime           int64   `json:"test_time" gorm:"bigint"`
	ResponseTime       int     `json:"response_time"` // in milliseconds
	BaseURL            *string `json:"base_url" gorm:"column:base_url;default:''"`
	Other              string  `json:"other"`
	Balance            float64 `json:"balance"` // in USD
	BalanceUpdatedTime int64   `json:"balance_updated_time" gorm:"bigint"`
	Models             string  `json:"models"`
	Group              string  `json:"group" gorm:"type:varchar(32);default:'default'"`
	UsedQuota          int64   `json:"used_quota" gorm:"bigint;default:0"`
	ModelMapping       *string `json:"model_mapping" gorm:"type:varchar(1024);default:''"`
	Priority           *int64  `json:"priority" gorm:"bigint;default:0"`
}

func GetAllChannels(ctx context.Context, startIdx int, num int, selectAll bool) ([]*Channel, error) {
	var channels []*Channel
	var err error
	if selectAll {
		err = DB.WithContext(ctx).Order("id desc").Find(&channels).Error
	} else {
		err = DB.WithContext(ctx).Order("id desc").Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}

func SearchChannels(ctx context.Context, keyword string) (channels []*Channel, err error) {
	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}
	err = DB.WithContext(ctx).Omit("key").Where("id = ? or name LIKE ? or "+keyCol+" = ?", common.String2Int(keyword), keyword+"%", keyword).Find(&channels).Error
	return channels, err
}

func GetChannelById(ctx context.Context, id int, selectAll bool) (*Channel, error) {
	channel := Channel{Id: id}
	var err error = nil
	if selectAll {
		err = DB.WithContext(ctx).First(&channel, "id = ?", id).Error
	} else {
		err = DB.WithContext(ctx).Omit("key").First(&channel, "id = ?", id).Error
	}
	return &channel, err
}

func BatchInsertChannels(ctx context.Context, channels []Channel) error {
	var err error
	err = DB.WithContext(ctx).Create(&channels).Error
	if err != nil {
		return err
	}
	for _, channel_ := range channels {
		err = channel_.AddAbilities(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) GetPriority() int64 {
	if channel.Priority == nil {
		return 0
	}
	return *channel.Priority
}

func (channel *Channel) GetBaseURL() string {
	if channel.BaseURL == nil {
		return ""
	}
	return *channel.BaseURL
}

func (channel *Channel) GetModelMapping() string {
	if channel.ModelMapping == nil {
		return ""
	}
	return *channel.ModelMapping
}

func (channel *Channel) Insert(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Create(channel).Error
	if err != nil {
		return err
	}
	err = channel.AddAbilities(ctx)
	return err
}

func (channel *Channel) Update(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Model(channel).Updates(channel).Error
	if err != nil {
		return err
	}
	DB.WithContext(ctx).Model(channel).First(channel, "id = ?", channel.Id)
	err = channel.UpdateAbilities(ctx)
	return err
}

func (channel *Channel) UpdateResponseTime(ctx context.Context, responseTime int64) {
	err := DB.WithContext(ctx).Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysError("failed to update response time: " + err.Error())
	}
}

func (channel *Channel) UpdateBalance(ctx context.Context, balance float64) {
	err := DB.WithContext(ctx).Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysError("failed to update balance: " + err.Error())
	}
}

func (channel *Channel) Delete(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Delete(channel).Error
	if err != nil {
		return err
	}
	err = channel.DeleteAbilities(ctx)
	return err
}

func UpdateChannelStatusById(ctx context.Context, id int, status int) {
	err := UpdateAbilityStatus(ctx, id, status == common.ChannelStatusEnabled)
	if err != nil {
		common.SysError("failed to update ability status: " + err.Error())
	}
	err = DB.WithContext(ctx).Model(&Channel{}).Where("id = ?", id).Update("status", status).Error
	if err != nil {
		common.SysError("failed to update channel status: " + err.Error())
	}
}

func UpdateChannelUsedQuota(ctx context.Context, id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeChannelUsedQuota, id, quota)
		return
	}
	updateChannelUsedQuota(ctx, id, quota)
}

func updateChannelUsedQuota(ctx context.Context, id int, quota int) {
	err := DB.WithContext(ctx).Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
	if err != nil {
		common.SysError("failed to update channel used quota: " + err.Error())
	}
}

func DeleteChannelByStatus(ctx context.Context, status int64) (int64, error) {
	result := DB.WithContext(ctx).Where("status = ?", status).Delete(&Channel{})
	return result.RowsAffected, result.Error
}

func DeleteDisabledChannel(ctx context.Context) (int64, error) {
	result := DB.WithContext(ctx).Where("status = ? or status = ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled).Delete(&Channel{})
	return result.RowsAffected, result.Error
}
