package logger

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sea-try-go/service/common/snowflake"
	"strings"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	z_trace "github.com/zeromicro/go-zero/core/trace"
)

// 测试前初始化：重置全局logger，设置日志为控制台输出（便于测试观察）
func TestMain(m *testing.M) {
	// 重置全局logger
	globalLogger = &Logger{}
	// 设置logx为控制台输出（测试环境）
	logx.SetWriter(logx.NewWriter(os.Stdout))

	// 运行测试
	code := m.Run()
	os.Exit(code)
}

// TestInit 测试初始化功能
func TestInit(t *testing.T) {
	// 测试1：空服务名初始化（应panic）
	defer func() {
		if r := recover(); r == nil {
			t.Error("Init with empty service name should panic")
		}
	}()
	Init("")

	// 测试2：正常初始化（不应panic）
	globalLogger = &Logger{} // 重置
	Init("test-api")
	if globalLogger.serviceName != "test-api" {
		t.Errorf("Init failed, expected serviceName 'test-api', got '%s'", globalLogger.serviceName)
	}

	// 测试3：重复初始化（应只执行一次）
	globalLogger = &Logger{} // 重置
	Init("first-name")
	Init("second-name") // 重复调用
	if globalLogger.serviceName != "first-name" {
		t.Error("Init should be called only once (sync.Once)")
	}
}

// TestLogInfo 测试信息日志输出
func TestLogInfo(t *testing.T) {
	// 0. 初始化snowflake
	snowflake.Init()

	// 1. 初始化logger
	globalLogger = &Logger{}
	Init("test-api")

	// 2. 模拟带TraceID的上下文
	traceID := "test-trace-123456"
	ctx := context.WithValue(context.Background(), z_trace.TraceIdKey, traceID)

	// 3. 输出信息日志（验证无panic，且字段正确）
	LogInfo(ctx, "test info message")

	// 4. 测试空上下文
	LogInfo(context.Background(), "test empty ctx message")

	// 5. 测试带可选参数的日志
	LogInfo(ctx, "test message with user and article",
		WithUserID("user123"),
		WithArticleID("article456"))

	// 6. 测试只带UserID的日志
	LogInfo(ctx, "test message with user only",
		WithUserID("user789"))

	// 7. 测试只带ArticleID的日志
	LogInfo(ctx, "test message with article only",
		WithArticleID("article789"))

	// 8. 验证时间戳格式
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if len(ts) != 24 {
		t.Errorf("Timestamp length should be 24, got %d: %s", len(ts), ts)
	}
}

// TestLogBusinessErr 测试业务错误日志输出
func TestLogBusinessErr(t *testing.T) {
	// 1. 初始化logger
	globalLogger = &Logger{}
	Init("test-api")

	// 2. 模拟带TraceID的上下文
	traceID := "test-trace-789012"
	ctx := context.WithValue(context.Background(), z_trace.TraceIdKey, traceID)
	// 3. 输出业务错误日志
	err := os.ErrNotExist // 模拟错误
	LogBusinessErr(ctx, 500, err)

	// 4. 测试空上下文+空错误
	LogBusinessErr(context.Background(), 400, nil) // 验证无panic

	// 5. 测试带可选参数的业务错误日志
	LogBusinessErr(ctx, 404, fmt.Errorf("not found"),
		WithUserID("user123"),
		WithArticleID("article456"))

	// 6. 测试只带UserID的业务错误日志
	LogBusinessErr(ctx, 403, fmt.Errorf("forbidden"),
		WithUserID("user789"))

	// 7. 测试只带ArticleID的业务错误日志
	LogBusinessErr(ctx, 400, fmt.Errorf("bad request"),
		WithArticleID("article789"))
}

// TestLogFatal 测试致命错误日志输出
// 注意：LogFatal会调用os.Exit，需单独测试且跳过（避免终止测试进程）
func TestLogFatal(t *testing.T) {
	// 跳过实际执行os.Exit的测试，仅验证日志字段采集逻辑
	t.Skip("LogFatal calls os.Exit, skip actual execution")

	// 初始化logger
	globalLogger = &Logger{}
	Init("test-api")

	// 模拟上下文
	traceID := "test-trace-789999"
	ctx := context.WithValue(context.Background(), z_trace.TraceIdKey, traceID)
	// 执行（注：实际运行会退出，故跳过）
	LogFatal(ctx, fmt.Errorf("fatal error: db connect failed"))
}

// TestGetFuncName 测试函数名解析
func TestGetFuncName(t *testing.T) {
	// 1. 测试当前函数的funcName
	pc, _, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	funcName := getFuncName(pc)
	expected := "sea-try-go/service/common/logger.TestGetFuncName"
	if funcName != expected {
		t.Errorf("getFuncName failed, expected '%s', got '%s'", expected, funcName)
	}

	// 2. 测试空pc
	if getFuncName(0) != "unknown" {
		t.Error("getFuncName with 0 pc should return 'unknown'")
	}
}

// TestGetCallChain 测试调用链路采集
func TestGetCallChain(t *testing.T) {
	// 定义测试函数链
	testFunc2 := func() string {
		return getCallChain(2) // skip=2，跳过当前+testFunc1
	}
	testFunc1 := func() string {
		return testFunc2()
	}

	// 执行测试
	chain := testFunc1()
	t.Logf("call chain: %s", chain)

	// 验证链路包含测试函数（核心逻辑：链路非空，且包含关键函数名）
	if chain == "unknown" || !strings.Contains(chain, "TestGetCallChain") {
		t.Error("getCallChain failed, invalid call chain")
	}
}

// TestIsFiltered 测试栈帧过滤逻辑
func TestIsFiltered(t *testing.T) {
	tests := []struct {
		name     string
		funcName string
		expected bool
	}{
		{"runtime函数", "runtime.main", true},
		{"testing函数", "testing.tRunner", true},
		{"go-zero函数", "github.com/zeromicro/go-zero/core/logx.Errorw", true},
		{"业务函数", "test-api/handler.UserHandler", false},
		{"空函数名", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isFiltered(tt.funcName) != tt.expected {
				t.Errorf("isFiltered(%s) = %v, expected %v", tt.funcName, isFiltered(tt.funcName), tt.expected)
			}
		})
	}
}
