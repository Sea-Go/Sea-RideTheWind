package svc

import (
	"context"
	"sea-try-go/service/article/rpc/internal/config"
	"sea-try-go/service/article/rpc/internal/model"
	"sea-try-go/service/common/snowflake"
	"sea-try-go/service/message/rpc/messageservice"
	"sea-try-go/service/security/rpc/client/contentsecurityservice"
	"sea-try-go/service/security/rpc/client/imagesecurityservice"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/zeromicro/go-queue/kq"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/zrpc"
)

type ServiceContext struct {
	Config            config.Config
	ArticleRepo       *model.ArticleRepo
	ArticleSyncOutbox model.ArticleSyncOutboxModel
	KqPusher          *kq.Pusher
	ArticleSyncPusher *kq.Pusher

	MinioClient      *minio.Client
	HotEventPusher   *kq.Pusher
	SecurityRpc      contentsecurityservice.ContentSecurityService
	ImageSecurityRpc imagesecurityservice.ImageSecurityService
	MessageRpc       messageservice.MessageService
}

func NewServiceContext(c config.Config, articleRepo *model.ArticleRepo) *ServiceContext {

	minioClient, err := minio.New(c.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.MinIO.AccessKeyID, c.MinIO.SecretAccessKey, ""),
		Secure: c.MinIO.UseSSL,
	})
	if err != nil {
		panic(err)
	}

	err = minioClient.MakeBucket(context.Background(), c.MinIO.BucketName, minio.MakeBucketOptions{})
	if err != nil {
		exists, errBucketExists := minioClient.BucketExists(context.Background(), c.MinIO.BucketName)
		if errBucketExists == nil && exists {
		} else {
			logx.Errorf("create minio bucket failed: %v", err)
		}
	} else {
		policy := `{"Version": "2012-10-17","Statement": [{"Action": ["s3:GetObject"],"Effect": "Allow","Principal": {"AWS": ["*"]},"Resource": ["arn:aws:s3:::` + c.MinIO.BucketName + `/*"],"Sid": ""}]}`
		err = minioClient.SetBucketPolicy(context.Background(), c.MinIO.BucketName, policy)
		if err != nil {
			logx.Errorf("set minio bucket policy failed: %v", err)
		}
	}

	snowflake.Init()

	securityClient := zrpc.MustNewClient(c.SecurityConf)
	return &ServiceContext{
		Config:            c,
		ArticleRepo:       articleRepo,
		ArticleSyncOutbox: model.NewArticleSyncOutboxModel(articleRepo.Db),
		KqPusher:          kq.NewPusher(c.KqPusherConf.Brokers, c.KqPusherConf.Topic),
		ArticleSyncPusher: kq.NewPusher(c.ArticleSyncPusherConf.Brokers, c.ArticleSyncPusherConf.Topic),
		MinioClient:       minioClient,
		HotEventPusher: kq.NewPusher(
			c.HotEventPusherConf.Brokers,
			c.HotEventPusherConf.Topic,
		),
		SecurityRpc:      contentsecurityservice.NewContentSecurityService(securityClient),
		ImageSecurityRpc: imagesecurityservice.NewImageSecurityService(securityClient),
		MessageRpc:       messageservice.NewMessageService(zrpc.MustNewClient(c.MessageRpc)),
	}
}
