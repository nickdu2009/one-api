package model

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"one-api/common"
)

type Token struct {
	Id             int    `json:"id"`
	UserId         int    `json:"user_id"`
	Key            string `json:"key" gorm:"type:char(48);uniqueIndex"`
	Status         int    `json:"status" gorm:"default:1"`
	Name           string `json:"name" gorm:"index" `
	CreatedTime    int64  `json:"created_time" gorm:"bigint"`
	AccessedTime   int64  `json:"accessed_time" gorm:"bigint"`
	ExpiredTime    int64  `json:"expired_time" gorm:"bigint;default:-1"` // -1 means never expired
	RemainQuota    int    `json:"remain_quota" gorm:"default:0"`
	UnlimitedQuota bool   `json:"unlimited_quota" gorm:"default:false"`
	UsedQuota      int    `json:"used_quota" gorm:"default:0"` // used quota
}

func GetAllUserTokens(ctx context.Context, userId int, startIdx int, num int) ([]*Token, error) {
	var tokens []*Token
	var err error
	err = DB.WithContext(ctx).Where("user_id = ?", userId).Order("id desc").Limit(num).Offset(startIdx).Find(&tokens).Error
	return tokens, err
}

func SearchUserTokens(ctx context.Context, userId int, keyword string) (tokens []*Token, err error) {
	err = DB.WithContext(ctx).Where("user_id = ?", userId).Where("name LIKE ?", keyword+"%").Find(&tokens).Error
	return tokens, err
}

func ValidateUserToken(ctx context.Context, key string) (token *Token, err error) {
	if key == "" {
		return nil, errors.New("未提供令牌")
	}
	token, err = CacheGetTokenByKey(ctx, key)
	if err == nil {
		if token.Status == common.TokenStatusExhausted {
			return nil, errors.New("该令牌额度已用尽")
		} else if token.Status == common.TokenStatusExpired {
			return nil, errors.New("该令牌已过期")
		}
		if token.Status != common.TokenStatusEnabled {
			return nil, errors.New("该令牌状态不可用")
		}
		if token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp() {
			if !common.RedisEnabled {
				token.Status = common.TokenStatusExpired
				err := token.SelectUpdate(ctx)
				if err != nil {
					common.SysError("failed to update token status" + err.Error())
				}
			}
			return nil, errors.New("该令牌已过期")
		}
		if !token.UnlimitedQuota && token.RemainQuota <= 0 {
			if !common.RedisEnabled {
				// in this case, we can make sure the token is exhausted
				token.Status = common.TokenStatusExhausted
				err := token.SelectUpdate(ctx)
				if err != nil {
					common.SysError("failed to update token status" + err.Error())
				}
			}
			return nil, errors.New("该令牌额度已用尽")
		}
		return token, nil
	}
	return nil, errors.New("无效的令牌")
}

func GetTokenByIds(ctx context.Context, id int, userId int) (*Token, error) {
	if id == 0 || userId == 0 {
		return nil, errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	var err error = nil
	err = DB.WithContext(ctx).First(&token, "id = ? and user_id = ?", id, userId).Error
	return &token, err
}

func GetTokenById(ctx context.Context, id int) (*Token, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	token := Token{Id: id}
	var err error = nil
	err = DB.WithContext(ctx).First(&token).Error
	return &token, err
}

func (token *Token) Insert(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Create(token).Error
	return err
}

// Update Make sure your token's fields is completed, because this will update non-zero values
func (token *Token) Update(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Model(token).Select("name", "status", "expired_time", "remain_quota", "unlimited_quota").Updates(token).Error
	return err
}

func (token *Token) SelectUpdate(ctx context.Context) error {
	// This can update zero values
	return DB.WithContext(ctx).Model(token).Select("accessed_time", "status").Updates(token).Error
}

func (token *Token) Delete(ctx context.Context) error {
	var err error
	err = DB.WithContext(ctx).Delete(token).Error
	return err
}

func DeleteTokenById(ctx context.Context, id int, userId int) (err error) {
	// Why we need userId here? In case user want to delete other's token.
	if id == 0 || userId == 0 {
		return errors.New("id 或 userId 为空！")
	}
	token := Token{Id: id, UserId: userId}
	err = DB.WithContext(ctx).Where(token).First(&token).Error
	if err != nil {
		return err
	}
	return token.Delete(ctx)
}

func IncreaseTokenQuota(ctx context.Context, id int, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, quota)
		return nil
	}
	return increaseTokenQuota(ctx, id, quota)
}

func increaseTokenQuota(ctx context.Context, id int, quota int) (err error) {
	err = DB.WithContext(ctx).Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", quota),
			"used_quota":    gorm.Expr("used_quota - ?", quota),
			"accessed_time": common.GetTimestamp(),
		},
	).Error
	return err
}

func DecreaseTokenQuota(ctx context.Context, id int, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeTokenQuota, id, -quota)
		return nil
	}
	return decreaseTokenQuota(ctx, id, quota)
}

func decreaseTokenQuota(ctx context.Context, id int, quota int) (err error) {
	err = DB.WithContext(ctx).Model(&Token{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		},
	).Error
	return err
}

func PreConsumeTokenQuota(ctx context.Context, tokenId int, quota int) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	token, err := GetTokenById(ctx, tokenId)
	if err != nil {
		return err
	}
	if !token.UnlimitedQuota && token.RemainQuota < quota {
		return errors.New("令牌额度不足")
	}
	userQuota, err := GetUserQuota(ctx, token.UserId)
	if err != nil {
		return err
	}
	if userQuota < quota {
		return errors.New("用户额度不足")
	}
	quotaTooLow := userQuota >= common.QuotaRemindThreshold && userQuota-quota < common.QuotaRemindThreshold
	noMoreQuota := userQuota-quota <= 0
	if quotaTooLow || noMoreQuota {
		go func() {
			email, err := GetUserEmail(ctx, token.UserId)
			if err != nil {
				common.SysError("failed to fetch user email: " + err.Error())
			}
			prompt := "您的额度即将用尽"
			if noMoreQuota {
				prompt = "您的额度已用尽"
			}
			if email != "" {
				topUpLink := fmt.Sprintf("%s/topup", common.ServerAddress)
				err = common.SendEmail(prompt, email,
					fmt.Sprintf("%s，当前剩余额度为 %d，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='%s'>%s</a>", prompt, userQuota, topUpLink, topUpLink))
				if err != nil {
					common.SysError("failed to send email" + err.Error())
				}
			}
		}()
	}
	if !token.UnlimitedQuota {
		err = DecreaseTokenQuota(ctx, tokenId, quota)
		if err != nil {
			return err
		}
	}
	err = DecreaseUserQuota(ctx, token.UserId, quota)
	return err
}

func PostConsumeTokenQuota(ctx context.Context, tokenId int, quota int) (err error) {
	token, err := GetTokenById(ctx, tokenId)
	if quota > 0 {
		err = DecreaseUserQuota(ctx, token.UserId, quota)
	} else {
		err = IncreaseUserQuota(ctx, token.UserId, -quota)
	}
	if err != nil {
		return err
	}
	if !token.UnlimitedQuota {
		if quota > 0 {
			err = DecreaseTokenQuota(ctx, tokenId, quota)
		} else {
			err = IncreaseTokenQuota(ctx, tokenId, -quota)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
