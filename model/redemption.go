package model

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"one-api/common"
)

type Redemption struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id"`
	Key          string `json:"key" gorm:"type:char(32);uniqueIndex"`
	Status       int    `json:"status" gorm:"default:1"`
	Name         string `json:"name" gorm:"index"`
	Quota        int    `json:"quota" gorm:"default:100"`
	CreatedTime  int64  `json:"created_time" gorm:"bigint"`
	RedeemedTime int64  `json:"redeemed_time" gorm:"bigint"`
	Count        int    `json:"count" gorm:"-:all"` // only for api request
}

func GetAllRedemptions(ctx context.Context, startIdx int, num int) ([]*Redemption, error) {
	var redemptions []*Redemption
	var err error
	err = DB.WithContext(ctx).Order("id desc").Limit(num).Offset(startIdx).Find(&redemptions).Error
	return redemptions, err
}

func SearchRedemptions(ctx context.Context, keyword string) (redemptions []*Redemption, err error) {
	err = DB.WithContext(ctx).Where("id = ? or name LIKE ?", keyword, keyword+"%").Find(&redemptions).Error
	return redemptions, err
}

func GetRedemptionById(ctx context.Context, id int) (*Redemption, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	var err error = nil
	err = DB.WithContext(ctx).First(&redemption, "id = ?", id).Error
	return &redemption, err
}

func Redeem(ctx context.Context, key string, userId int) (quota int, err error) {
	if key == "" {
		return 0, errors.New("未提供兑换码")
	}
	if userId == 0 {
		return 0, errors.New("无效的 user id")
	}
	redemption := &Redemption{}

	keyCol := "`key`"
	if common.UsingPostgreSQL {
		keyCol = `"key"`
	}

	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Set("gorm:query_option", "FOR UPDATE").Where(keyCol+" = ?", key).First(redemption).Error
		if err != nil {
			return errors.New("无效的兑换码")
		}
		if redemption.Status != common.RedemptionCodeStatusEnabled {
			return errors.New("该兑换码已被使用")
		}
		err = tx.Model(&User{}).Where("id = ?", userId).Update("quota", gorm.Expr("quota + ?", redemption.Quota)).Error
		if err != nil {
			return err
		}
		redemption.RedeemedTime = common.GetTimestamp()
		redemption.Status = common.RedemptionCodeStatusUsed
		err = tx.Save(redemption).Error
		return err
	})
	if err != nil {
		return 0, errors.New("兑换失败，" + err.Error())
	}
	RecordLog(ctx, userId, LogTypeTopup, fmt.Sprintf("通过兑换码充值 %s", common.LogQuota(redemption.Quota)))
	return redemption.Quota, nil
}

func (redemption *Redemption) Insert(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Create(redemption).Error
	return err
}

func (redemption *Redemption) SelectUpdate(ctx context.Context) error {
	// This can update zero values
	return DB.WithContext(ctx).Model(redemption).Select("redeemed_time", "status").Updates(redemption).Error
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (redemption *Redemption) Update(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Model(redemption).Select("name", "status", "quota", "redeemed_time").Updates(redemption).Error
	return err
}

func (redemption *Redemption) Delete(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Delete(redemption).Error
	return err
}

func DeleteRedemptionById(ctx context.Context, id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	redemption := Redemption{Id: id}
	err = DB.WithContext(ctx).Where(redemption).First(&redemption).Error
	if err != nil {
		return err
	}
	return redemption.Delete(ctx)
}
