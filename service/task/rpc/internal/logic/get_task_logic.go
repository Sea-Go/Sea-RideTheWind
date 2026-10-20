package logic

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/internal/Model"
	__ "github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/pb"
	"strconv"
	"strings"
	"time"

	"github.com/Sea-Go/Sea-RideTheWind/service/task/rpc/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetTaskLogic struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
}

func NewGetTaskLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetTaskLogic {
	return &GetTaskLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
	}
}

/*func (l *GetTaskLogic) GetTask(in *__.GetTaskReq) (*__.GetTaskResp, error) {

	return &__.GetTaskResp{}, nil
}
*/

func (l *GetTaskLogic) GetTask(in *__.GetTaskReq) (*__.GetTaskResp, error) {
	ctx := l.ctx
	uid := in.GetUserId()
	if uid <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id invalid")
	}

	// 1) Redis：一次 HGETALL 拿到该用户所有 task 的进度
	pkey := buildUserProgressKey(uid)
	pm, rerr := l.svcCtx.Rdb.HGetAll(ctx, pkey).Result()
	// 2) miss：用 GORM 查该用户所有进度，并回填 Redis
	if rerr != nil || len(pm) == 0 {
		var rows []Model.TaskProgress
		err := l.svcCtx.Gdb.WithContext(ctx).
			Model(&Model.TaskProgress{}).
			Select("user_id, task_id, status, progress, target").
			Where("user_id = ?", uid).
			Find(&rows).Error
		if err != nil {
			return nil, err
		}

		pm = make(map[string]string, len(rows))
		if len(rows) > 0 {
			kv := make(map[string]any, len(rows))
			for _, r := range rows {
				// value = "status|progress|target"
				v := packProgress(r.Status, r.Progress, r.Target)
				f := strconv.FormatInt(r.TaskID, 10)
				pm[f] = v
				kv[f] = v
			}

			pipe := l.svcCtx.Rdb.Pipeline()
			pipe.HSet(ctx, pkey, kv)
			pipe.Expire(ctx, pkey, taskProgressTTL())
			_, _ = pipe.Exec(ctx) // 回填失败不影响返回
		} else {
			_ = l.svcCtx.Rdb.Expire(ctx, pkey, 30*time.Second).Err()
		}
	}

	// 3) merge：对每个任务定义都返回一个 Task（未开始默认 progress=0）
	out := make([]*__.Task, 0, len(Model.AllTaskDefs))
	for _, d := range Model.AllTaskDefs {
		var progress int64 = 0
		// required_progress 来自任务定义（更合理，不要用 progress 表的 target 覆盖 defs）
		required := d.RequiredProgress

		if packed, ok := pm[strconv.FormatInt(d.TaskID, 10)]; ok {
			_, p, _ := unpackProgress(packed) // status/target 目前 proto 用不上
			progress = p
		}

		out = append(out, &__.Task{
			Name:               d.Name,
			Desc:               d.Desc,
			TaskId:             d.TaskID,
			CompletionProgress: progress,
			RequiredProgress:   required,
		})
	}

	return &__.GetTaskResp{Task: out}, nil
}

func buildUserProgressKey(uid int64) string {
	return "task:progress:" + strconv.FormatInt(uid, 10)
}

func taskProgressTTL() time.Duration {
	return 15 * time.Minute
}

// value="status|progress|target"
func packProgress(status string, progress, target int64) string {
	// status 不要包含 '|'
	return status + "|" + strconv.FormatInt(progress, 10) + "|" + strconv.FormatInt(target, 10)
}

func unpackProgress(s string) (status string, progress int64, target int64) {
	parts := strings.Split(s, "|")
	if len(parts) != 3 {
		return "", 0, 0
	}
	status = parts[0]
	progress = parseInt64Default(parts[1], 0)
	target = parseInt64Default(parts[2], 0)
	return
}

func parseInt64Default(s string, def int64) int64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return v
}
