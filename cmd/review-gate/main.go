package main

import (
	"bucking.cn/code-review/internal/feishu"
	"bucking.cn/code-review/internal/gate"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		log.Print(e)
		os.Exit(1)
	}
}
func run() error {
	fs := flag.NewFlagSet("review-gate", flag.ContinueOnError)
	path := fs.String("config", "review-gate.json", "configuration file")
	if e := fs.Parse(os.Args[1:]); e != nil {
		return e
	}
	args := fs.Args()
	if len(args) == 0 {
		return fmt.Errorf("command required: once, run, preflight, protect, merge")
	}
	cfg, e := gate.LoadConfig(*path)
	if e != nil {
		return e
	}
	token := os.Getenv(cfg.TokenEnv)
	if token == "" {
		return fmt.Errorf("Gitea token environment variable is unset")
	}
	api := gate.NewAPI(cfg, token)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch args[0] {
	case "preflight":
		if e = api.Preflight(ctx); e != nil {
			return e
		}
		fmt.Println("Gitea identity and strict branch protection verified")
		return nil
	case "protect":
		flags := flag.NewFlagSet("protect", flag.ContinueOnError)
		apply := flags.Bool("apply", false, "apply protection and verify readback")
		if e = flags.Parse(args[1:]); e != nil {
			return e
		}
		plan, e := api.Protect(ctx, *apply)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(plan)
	}
	store, e := gate.OpenStore(cfg.StateDir)
	if e != nil {
		return e
	}
	defer store.Close()
	c := &gate.Controller{Config: cfg, API: api, Store: store, Reviewer: gate.SubprocessReviewer{Config: cfg}}
	id, secret := os.Getenv(cfg.FeishuAppIDEnv), os.Getenv(cfg.FeishuAppSecretEnv)
	if cfg.NotificationMode == "feishu_dm" && id != "" && secret != "" {
		c.Notifier = feishu.NewClient(id, secret)
	}
	if cfg.NotificationMode == "feishu_group" {
		if webhook := os.Getenv(cfg.FeishuWebhookURLEnv); webhook != "" {
			group := feishu.NewGroupClient(webhook)
			if keyword := os.Getenv(cfg.FeishuWebhookKeywordEnv); keyword != "" {
				group.Keyword = keyword
			}
			c.Notifier = group
		}
	}
	switch args[0] {
	case "once":
		if e = api.Preflight(ctx); e != nil {
			return e
		}
		return c.Once(ctx)
	case "run":
		if e = api.Preflight(ctx); e != nil {
			return e
		}
		for {
			if e = c.Once(ctx); e != nil {
				log.Print(e)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Duration(cfg.PollSeconds) * time.Second):
			}
		}
	case "retry":
		flags := flag.NewFlagSet("retry", flag.ContinueOnError)
		pr := flags.Int("pr", 0, "failed pull request number")
		if e = flags.Parse(args[1:]); e != nil {
			return e
		}
		return c.Retry(ctx, *pr)
	case "merge":
		flags := flag.NewFlagSet("merge", flag.ContinueOnError)
		pr := flags.Int("pr", 0, "pull request number")
		head := flags.String("head", "", "expected head SHA")
		if e = flags.Parse(args[1:]); e != nil {
			return e
		}
		return c.Merge(ctx, *pr, *head)
	default:
		return fmt.Errorf("unknown command")
	}
}
