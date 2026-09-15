package common

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	Success = 200
	Error   = 500

	// 通用错误码 5001-5099
	CodeServerBusy    = 1015
	ErrorServerCommon = 5001
	ErrorNotFound     = 5002
	ErrorInvalidParam = 5003
	ErrorUnauthorized = 5004
	ErrorForbidden    = 5005
	ErrorAlreadyExist = 5006
	ErrorDbSelect     = 5007
	ErrorDbUpdate     = 5008
	ErrorGenerateID   = 5009

	// 用户模块 5100-5199
	ErrorUserNotFound    = 5101
	ErrorUserExists      = 5102
	ErrorPasswordInvalid = 5103
	ErrorTokenInvalid    = 5104
	ErrorTokenExpired    = 5105

	// 收藏夹模块 5200-5299
	ErrorFavoriteFolderNotFound   = 5201
	ErrorFavoriteFolderNameEmpty  = 5202
	ErrorFavoriteFolderNameExists = 5203

	// 收藏模块 5300-5399
	ErrorFavoriteNotFound       = 5301
	ErrorFavoriteAlreadyExist   = 5302
	ErrorFavoriteTargetEmpty    = 5303
	ErrorFavoriteHistoryBlocked = 5304
)

var codeMsg = map[int]string{
	Success: "OK",
	Error:   "FAIL",

	// 通用
	CodeServerBusy:    "服务繁忙",
	ErrorServerCommon: "系统内部错误",
	ErrorNotFound:     "not found",
	ErrorInvalidParam: "参数错误",
	ErrorUnauthorized: "未登录或登录已失效",
	ErrorForbidden:    "无权限操作",
	ErrorAlreadyExist: "记录已存在",
	ErrorDbSelect:     "数据库查询失败",
	ErrorDbUpdate:     "数据库更新失败",
	ErrorGenerateID:   "ID生成失败",

	// 用户
	ErrorUserNotFound:    "用户不存在",
	ErrorUserExists:      "用户已存在",
	ErrorPasswordInvalid: "密码错误",
	ErrorTokenInvalid:    "token无效",
	ErrorTokenExpired:    "token已过期",

	// 收藏夹
	ErrorFavoriteFolderNotFound:   "收藏夹不存在",
	ErrorFavoriteFolderNameEmpty:  "收藏夹名称不能为空",
	ErrorFavoriteFolderNameExists: "收藏夹名称已存在",

	// 收藏
	ErrorFavoriteNotFound:       "收藏记录不存在",
	ErrorFavoriteAlreadyExist:   "该内容已收藏",
	ErrorFavoriteTargetEmpty:    "收藏目标不能为空",
	ErrorFavoriteHistoryBlocked: "收藏暂不可撤回",
}

var bizCodeByMsg = func() map[string]int {
	lookup := make(map[string]int, len(codeMsg))
	for code, msg := range codeMsg {
		if code == Success || code == Error {
			continue
		}
		if _, exists := lookup[msg]; !exists {
			lookup[msg] = code
		}
	}
	return lookup
}()

func GetErrMsg(code int) string {
	msg, ok := codeMsg[code]
	if !ok {
		return codeMsg[Error]
	}
	return msg
}

func bizCodeFromMessage(msg string) (int, bool) {
	code, ok := bizCodeByMsg[msg]
	return code, ok
}

func GRPCError(grpcCode codes.Code, bizCode int) error {
	return status.Error(grpcCode, GetErrMsg(bizCode))
}

func BizCodeFromError(err error) int {
	if err == nil {
		return Success
	}

	st, ok := status.FromError(err)
	if !ok {
		return CodeServerBusy
	}

	if code, ok := bizCodeFromMessage(st.Message()); ok {
		return code
	}

	switch st.Code() {
	case codes.InvalidArgument:
		return ErrorInvalidParam
	case codes.Unauthenticated:
		return ErrorUnauthorized
	case codes.PermissionDenied:
		return ErrorForbidden
	case codes.AlreadyExists:
		return ErrorAlreadyExist
	case codes.NotFound:
		return ErrorNotFound
	case codes.Internal:
		return ErrorServerCommon
	default:
		return CodeServerBusy
	}
}
