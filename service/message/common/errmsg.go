package common

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type CodeError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *CodeError) Error() string {
	return fmt.Sprintf("ErrCode:%d, Errmsg:%s", e.Code, e.Msg)
}

func NewErrCode(code int) error {
	return &CodeError{Code: code, Msg: GetErrMsg(code)}
}

func NewErrCodeMsg(code int, msg string) error {
	return &CodeError{Code: code, Msg: msg}
}

const (
	Success = 200
	Error   = 500

	CodeServerBusy        = 1015
	ErrorServerCommon     = 5001
	ErrorNotFound         = 5002
	ErrorInvalidParam     = 5003
	ErrorUnauthorized     = 5004
	ErrorForbidden        = 5005
	ErrorAlreadyExist     = 5006
	ErrorDbSelect         = 5007
	ErrorDbUpdate         = 5008
	ErrorUserNotFound     = 5101
	ErrorAdminNotFound    = 5102
	ErrorSelfChat         = 5201
	ErrorConversationMiss = 5202
	ErrorNotificationMiss = 5203
	ErrorChatPending      = 5204
	ErrorAdminOnlyInit    = 5205
	ErrorFirstMsgTooLong  = 5206
	ErrorUnsupportedRole  = 5207
)

var codeMsg = map[int]string{
	Success: "OK",
	Error:   "FAIL",

	CodeServerBusy:        "service busy",
	ErrorServerCommon:     "internal server error",
	ErrorNotFound:         "record not found",
	ErrorInvalidParam:     "invalid parameter",
	ErrorUnauthorized:     "unauthorized",
	ErrorForbidden:        "forbidden",
	ErrorAlreadyExist:     "already exists",
	ErrorDbSelect:         "database query failed",
	ErrorDbUpdate:         "database update failed",
	ErrorUserNotFound:     "user not found",
	ErrorAdminNotFound:    "admin not found",
	ErrorSelfChat:         "self chat is not allowed",
	ErrorConversationMiss: "conversation not found",
	ErrorNotificationMiss: "notification not found",
	ErrorChatPending:      "waiting for the other side to reply",
	ErrorAdminOnlyInit:    "only admin can initiate this conversation",
	ErrorFirstMsgTooLong:  "first private message cannot exceed 50 characters",
	ErrorUnsupportedRole:  "unsupported sender role",
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
	if msg, ok := codeMsg[code]; ok {
		return msg
	}
	return codeMsg[Error]
}

func GRPCError(grpcCode codes.Code, bizCode int) error {
	return status.Error(grpcCode, GetErrMsg(bizCode))
}

func BizCodeFromError(err error) int {
	if err == nil {
		return Success
	}

	if codeErr, ok := err.(*CodeError); ok {
		return codeErr.Code
	}

	st, ok := status.FromError(err)
	if !ok {
		return CodeServerBusy
	}

	if code, ok := bizCodeByMsg[st.Message()]; ok {
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
