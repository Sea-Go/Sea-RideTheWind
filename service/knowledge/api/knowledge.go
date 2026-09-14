package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sea-try-go/service/knowledge/api/internal/mqs"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/proc"

	"sea-try-go/service/knowledge/api/internal/config"
	"sea-try-go/service/knowledge/api/internal/handler"
	"sea-try-go/service/knowledge/api/internal/svc"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

func main() {
	configFile := flag.String("f", "service/knowledge/api/etc/knowledge-api.yaml", "configuration path")
	flag.Parse()
	var c config.Config
	if err := conf.Load(*configFile, &c, conf.UseEnv()); err != nil {
		fmt.Fprintln(os.Stderr, "load knowledge configuration:", err)
		os.Exit(1)
	}
	if err := run(c); err != nil {
		fmt.Fprintln(os.Stderr, "start knowledge:", err)
		os.Exit(1)
	}
}
func run(c config.Config) error {
	ctx, err := svc.NewServiceContext(c)
	if err != nil {
		return err
	}
	defer ctx.Close()
	lifecycle, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	defer workers.Wait()
	defer cancel()
	proc.AddShutdownListener(cancel)
	if c.Delivery.Enabled {
		if c.Delivery.Endpoint == "" || c.Delivery.IntervalMillis < 100 {
			return fmt.Errorf("delivery endpoint and interval >= 100ms required")
		}
		sender := &mqs.HTTPSender{Endpoint: c.Delivery.Endpoint, Token: c.Delivery.Token, Client: &http.Client{Timeout: 10 * time.Second}}
		workers.Add(1)
		go func() {
			defer workers.Done()
			mqs.Run(lifecycle, ctx.Store, sender, time.Duration(c.Delivery.IntervalMillis)*time.Millisecond)
		}()
	}
	handler.ConfigureResponses()
	server, err := rest.NewServer(c.RestConf)
	if err != nil {
		return err
	}
	defer server.Stop()
	handler.RegisterHandlers(server, ctx)
	server.Start()
	return nil
}
