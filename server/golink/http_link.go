// Package golink 连接
package golink

import (
	"fmt"
	"github.com/Runner-Go-Team/RunnerGo-engine-open/constant"
	"github.com/Runner-Go-Team/RunnerGo-engine-open/log"
	"github.com/Runner-Go-Team/RunnerGo-engine-open/middlewares"
	"github.com/Runner-Go-Team/RunnerGo-engine-open/model"
	"github.com/Runner-Go-Team/RunnerGo-engine-open/server/client"
	uuid "github.com/satori/go.uuid"
	"github.com/valyala/fasthttp"
	"go.mongodb.org/mongo-driver/mongo"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// HttpSend 发送http请求
func HttpSend(event model.Event, api model.Api, globalVar *sync.Map, requestCollection *mongo.Collection) (bool, int64, uint64, float64, float64, string, time.Time, time.Time) {
	var (
		isSucceed       = true
		errCode         = constant.NoError
		receivedBytes   = float64(0)
		errMsg          = ""
		assertNum       = 0
		assertFailedNum = 0
	)

	if api.Request.HttpApiSetup == nil {
		api.Request.HttpApiSetup = new(model.HttpApiSetup)
	}

	resp, req, requestTime, sendBytes, err, str, startTime, endTime := client.HTTPRequest(api.Request.Method, api.Request.URL, api.Request.Body, api.Request.Query,
		api.Request.Header, api.Request.Cookie, api.Request.Auth, api.Request.HttpApiSetup)
	defer fasthttp.ReleaseResponse(resp) // 用完需要释放资源
	defer fasthttp.ReleaseRequest(req)
	var regex []map[string]interface{}
	if api.Request.Regex != nil {
		for _, regular := range api.Request.Regex {
			if regular.IsChecked != constant.Open {
				continue
			}
			reg := make(map[string]interface{})
			value := regular.Extract(resp, globalVar)
			if value == nil {
				continue
			}
			reg[regular.Var] = value
			regex = append(regex, reg)
		}
	}
	if err != nil {
		isSucceed = false
		errMsg = err.Error()
	}
	var assertionMsgList []model.AssertionMsg
	// 断言验证

	if api.Request.Assert != nil {
		var assertionMsg = model.AssertionMsg{}
		var (
			code    = int64(10000)
			succeed = true
			msg     = ""
		)
		for _, v := range api.Request.Assert {
			if v.IsChecked != constant.Open {
				continue
			}
			code, succeed, msg = v.VerifyAssertionText(resp)
			if succeed != true {
				errCode = code
				isSucceed = succeed
				errMsg = msg
				assertFailedNum++
			}
			assertionMsg.Code = code
			assertionMsg.IsSucceed = succeed
			assertionMsg.Msg = msg
			assertionMsgList = append(assertionMsgList, assertionMsg)
			assertNum++
		}
	}
	// 接收到的字节长度
	//contentLength = uint(resp.Header.ContentLength())

	receivedBytes = float64(resp.Header.ContentLength()) / 1024
	if receivedBytes <= 0 {
		receivedBytes = float64(len(resp.Body())) / 1024
	}
	// 开启debug模式后，将请求响应信息写入到mongodb中
	if api.Debug != "" && api.Debug != "stop" {
		var debugMsg = make(map[string]interface{})
		responseTime := endTime.Format("2006-01-02 15:04:05")
		insertDebugMsg(regex, debugMsg, api.Debug, event, api, resp, req, requestTime, responseTime, receivedBytes, errMsg, str, err, isSucceed, assertionMsgList, assertNum, assertFailedNum)
		if requestCollection != nil {
			model.Insert(requestCollection, debugMsg, middlewares.LocalIp)
		}
	}
	return isSucceed, errCode, requestTime, sendBytes, receivedBytes, errMsg, startTime, endTime
}

func insertDebugMsg(regex []map[string]interface{}, debugMsg map[string]interface{}, debugType string, event model.Event, api model.Api, resp *fasthttp.Response, req *fasthttp.Request, requestTime uint64, responseTime string, receivedBytes float64, errMsg, str string, err error, isSucceed bool, assertionMsgList []model.AssertionMsg, assertNum, assertFailedNum int) {
	switch debugType {
	case constant.All:
		makeDebugMsg(regex, debugMsg, event, api, resp, req, requestTime, responseTime, receivedBytes, errMsg, str, err, isSucceed, assertionMsgList, assertNum, assertFailedNum)
	case constant.OnlySuccess:
		if isSucceed == true {
			makeDebugMsg(regex, debugMsg, event, api, resp, req, requestTime, responseTime, receivedBytes, errMsg, str, err, isSucceed, assertionMsgList, assertNum, assertFailedNum)
		}

	case constant.OnlyError:
		if isSucceed == false {
			makeDebugMsg(regex, debugMsg, event, api, resp, req, requestTime, responseTime, receivedBytes, errMsg, str, err, isSucceed, assertionMsgList, assertNum, assertFailedNum)
		}
	}
}

func makeDebugMsg(regex []map[string]interface{}, debugMsg map[string]interface{}, event model.Event, api model.Api, resp *fasthttp.Response, req *fasthttp.Request, requestTime uint64, responseTime string, receivedBytes float64, errMsg, str string, err error, isSucceed bool, assertionMsgList []model.AssertionMsg, assertNum, assertFailedNum int) {
	debugMsg["team_id"] = event.TeamId
	debugMsg["request_url"] = req.URI().String()
	debugMsg["plan_id"] = event.PlanId
	debugMsg["report_id"] = event.ReportId
	debugMsg["scene_id"] = event.SceneId
	debugMsg["parent_id"] = event.ParentId
	debugMsg["case_id"] = event.CaseId
	if api.Uuid.String() == "00000000-0000-0000-0000-000000000000" {
		api.Uuid = uuid.NewV4()
	}
	debugMsg["uuid"] = api.Uuid.String()
	debugMsg["event_id"] = event.Id
	debugMsg["api_id"] = api.TargetId
	debugMsg["api_name"] = api.Name
	if req.Header.Method() != nil {
		debugMsg["method"] = string(req.Header.Method())
	}
	debugMsg["type"] = constant.RequestType
	debugMsg["request_time"] = requestTime / uint64(time.Millisecond)
	debugMsg["request_code"] = resp.StatusCode()
	debugMsg["request_header"] = req.Header.String()
	debugMsg["response_time"] = responseTime
	if string(req.Body()) != "" {
		var errBody error
		debugMsg["request_body"], errBody = url.QueryUnescape(string(req.Body()))
		if errBody != nil {
			debugMsg["request_body"] = string(req.Body())
		}
		log.Logger.Debug()
	} else {
		debugMsg["request_body"] = str
	}
	if string(resp.Body()) == "" && errMsg != "" {
		debugMsg["response_body"] = errMsg
	}

	debugMsg["response_header"] = resp.Header.String()

	debugMsg["response_bytes"], _ = strconv.ParseFloat(fmt.Sprintf("%0.2f", receivedBytes), 64)
	if err != nil {
		debugMsg["response_body"] = err.Error()
	} else {
		debugMsg["response_body"] = string(resp.Body())
	}
	switch isSucceed {
	case false:
		debugMsg["status"] = constant.Failed
	case true:
		debugMsg["status"] = constant.Success
	}

	debugMsg["next_list"] = event.NextList
	debugMsg["assert"] = assertionMsgList
	debugMsg["assert_num"] = assertNum
	debugMsg["assert_failed_num"] = assertFailedNum
	debugMsg["regex"] = regex
}
