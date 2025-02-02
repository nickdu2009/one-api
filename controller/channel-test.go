package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"one-api/common"
	"one-api/model"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func testChannel(channel *model.Channel, request ChatRequest) (err error, openaiErr *OpenAIError) {
	switch channel.Type {
	case common.ChannelTypePaLM:
		fallthrough
	case common.ChannelTypeAnthropic:
		fallthrough
	case common.ChannelTypeBaidu:
		fallthrough
	case common.ChannelTypeZhipu:
		fallthrough
	case common.ChannelTypeAli:
		fallthrough
	case common.ChannelType360:
		fallthrough
	case common.ChannelTypeXunfei:
		return errors.New("该渠道类型当前版本不支持测试，请手动测试"), nil
	case common.ChannelTypeAzure:
		request.Model = "gpt-35-turbo"
		defer func() {
			if err != nil {
				err = errors.New("请确保已在 Azure 上创建了 gpt-35-turbo 模型，并且 apiVersion 已正确填写！")
			}
		}()
	default:
		request.Model = "gpt-3.5-turbo"
	}
	requestURL := common.ChannelBaseURLs[channel.Type]
	if channel.Type == common.ChannelTypeAzure {
		requestURL = getFullRequestURL(channel.GetBaseURL(), fmt.Sprintf("/openai/deployments/%s/chat/completions?api-version=2023-03-15-preview", request.Model), channel.Type)
	} else {
		if baseURL := channel.GetBaseURL(); len(baseURL) > 0 {
			requestURL = baseURL
		}

		requestURL = getFullRequestURL(requestURL, "/v1/chat/completions", channel.Type)
	}
	jsonData, err := json.Marshal(request)
	if err != nil {
		return err, nil
	}
	req, err := http.NewRequest("POST", requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return err, nil
	}
	if channel.Type == common.ChannelTypeAzure {
		req.Header.Set("api-key", channel.Key)
	} else {
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err, nil
	}
	defer resp.Body.Close()
	var response TextResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err, nil
	}
	err = json.Unmarshal(body, &response)
	if err != nil {
		return fmt.Errorf("Error: %s\nResp body: %s", err, body), nil
	}
	if response.Usage.CompletionTokens == 0 {
		if response.Error.Message == "" {
			response.Error.Message = "补全 tokens 非预期返回 0"
		}
		return errors.New(fmt.Sprintf("type %s, code %v, message %s", response.Error.Type, response.Error.Code, response.Error.Message)), &response.Error
	}
	return nil, nil
}

func buildTestRequest() *ChatRequest {
	testRequest := &ChatRequest{
		Model:     "", // this will be set later
		MaxTokens: 1,
	}
	testMessage := Message{
		Role:    "user",
		Content: "hi",
	}
	testRequest.Messages = append(testRequest.Messages, testMessage)
	return testRequest
}

func TestChannel(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel, err := model.GetChannelById(ctx, id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	testRequest := buildTestRequest()
	tik := time.Now()
	err, _ = testChannel(channel, *testRequest)
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	go channel.UpdateResponseTime(ctx, milliseconds)
	consumedTime := float64(milliseconds) / 1000.0
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
			"time":    consumedTime,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"time":    consumedTime,
	})
	return
}

var testAllChannelsLock sync.Mutex
var testAllChannelsRunning bool = false

func notifyRootUser(ctx context.Context, subject string, content string) {
	if common.RootUserEmail == "" {
		common.RootUserEmail = model.GetRootUserEmail(ctx)
	}
	err := common.SendEmail(subject, common.RootUserEmail, content)
	if err != nil {
		common.SysError(fmt.Sprintf("failed to send email: %s", err.Error()))
	}
}

// disable & notify
func disableChannel(ctx context.Context, channelId int, channelName string, reason string) {
	model.UpdateChannelStatusById(ctx, channelId, common.ChannelStatusAutoDisabled)
	subject := fmt.Sprintf("通道「%s」（#%d）已被禁用", channelName, channelId)
	content := fmt.Sprintf("通道「%s」（#%d）已被禁用，原因：%s", channelName, channelId, reason)
	notifyRootUser(ctx, subject, content)
}

// enable & notify
func enableChannel(ctx context.Context, channelId int, channelName string) {
	model.UpdateChannelStatusById(ctx, channelId, common.ChannelStatusEnabled)
	subject := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
	content := fmt.Sprintf("通道「%s」（#%d）已被启用", channelName, channelId)
	notifyRootUser(ctx, subject, content)
}

func testAllChannels(ctx context.Context, notify bool) error {
	if common.RootUserEmail == "" {
		common.RootUserEmail = model.GetRootUserEmail(ctx)
	}
	testAllChannelsLock.Lock()
	if testAllChannelsRunning {
		testAllChannelsLock.Unlock()
		return errors.New("测试已在运行中")
	}
	testAllChannelsRunning = true
	testAllChannelsLock.Unlock()
	channels, err := model.GetAllChannels(ctx, 0, 0, true)
	if err != nil {
		return err
	}
	testRequest := buildTestRequest()
	var disableThreshold = int64(common.ChannelDisableThreshold * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000 // a impossible value
	}
	go func() {
		for _, channel := range channels {
			isChannelEnabled := channel.Status == common.ChannelStatusEnabled
			tik := time.Now()
			err, openaiErr := testChannel(channel, *testRequest)
			tok := time.Now()
			milliseconds := tok.Sub(tik).Milliseconds()
			if isChannelEnabled && milliseconds > disableThreshold {
				err = errors.New(fmt.Sprintf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0))
				disableChannel(ctx, channel.Id, channel.Name, err.Error())
			}
			if isChannelEnabled && shouldDisableChannel(openaiErr, -1) {
				disableChannel(ctx, channel.Id, channel.Name, err.Error())
			}
			if !isChannelEnabled && shouldEnableChannel(err, openaiErr) {
				enableChannel(ctx, channel.Id, channel.Name)
			}
			channel.UpdateResponseTime(ctx, milliseconds)
			time.Sleep(common.RequestInterval)
		}
		testAllChannelsLock.Lock()
		testAllChannelsRunning = false
		testAllChannelsLock.Unlock()
		if notify {
			err := common.SendEmail("通道测试完成", common.RootUserEmail, "通道测试完成，如果没有收到禁用通知，说明所有通道都正常")
			if err != nil {
				common.SysError(fmt.Sprintf("failed to send email: %s", err.Error()))
			}
		}
	}()
	return nil
}

func TestAllChannels(c *gin.Context) {
	ctx := c.Request.Context()
	err := testAllChannels(ctx, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AutomaticallyTestChannels(ctx context.Context, frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		common.SysLog("testing all channels")
		_ = testAllChannels(ctx, false)
		common.SysLog("channel test finished")
	}
}
