// FasterEdge 开源项目 · https://github.com/FasterEdge · https://gitee.com/FasterEdge
// SimpleWebShell-RemoteUpdate performs non-invasive remote release workflows through an existing SimpleWebShell.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"simplewebshell-remoteupdate/internal/config"
	"simplewebshell-remoteupdate/internal/webshell"
	"simplewebshell-remoteupdate/internal/workflow"
)

var version = "1.0.20260902"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	global := flag.NewFlagSet("simplewebshell-remoteupdate", flag.ContinueOnError)
	global.SetOutput(os.Stderr)
	configPath := global.String("config", "remoteupdate.json", "JSON 配置文件")
	dryRun := global.Bool("dry-run", false, "只打印流程，不执行远程变更")
	showVersion := global.Bool("version", false, "显示版本")
	if err := global.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Printf("SimpleWebShell-RemoteUpdate %s\n", version)
		return 0
	}
	rest := global.Args()
	if len(rest) == 0 {
		usage()
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("配置错误: %v", err)
		return 2
	}
	client := webshell.New(cfg.Remote.URL, cfg.Remote.Key, cfg.Remote.Timeout, cfg.Remote.InsecureTLS)
	engine := &workflow.Engine{
		Config: cfg,
		Remote: client,
		DryRun: *dryRun,
		Log: func(format string, values ...any) {
			log.Printf(format, values...)
		},
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if !*dryRun {
		if err := engine.Begin(ctx); err != nil {
			log.Printf("连接失败: %v", err)
			return 1
		}
		defer engine.End(context.Background())
	}

	cmd := rest[0]
	cmdArgs := rest[1:]
	switch cmd {
	case "probe":
		if *dryRun {
			log.Print("dry-run: 跳过远程探测")
		} else {
			log.Print("SimpleWebShell 连接与密钥验证成功")
		}
	case "init":
		err = engine.Init(ctx)
	case "install", "update":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		artifact := fs.String("artifact", "", "本地发布包路径")
		ver := fs.String("release", "", "版本号（留空自动生成UTC时间版本）")
		if parseErr := fs.Parse(cmdArgs); parseErr != nil {
			return 2
		}
		if *ver == "" {
			*ver = workflow.NowVersion()
		}
		if cmd == "install" {
			err = engine.Install(ctx, *ver, *artifact)
		} else {
			err = engine.Update(ctx, *ver, *artifact)
		}
	case "status":
		var status *workflow.Status
		status, err = engine.GetStatus(ctx)
		if err == nil {
			fmt.Println(workflow.MarshalStatus(status))
		}
	case "rollback":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		ver := fs.String("release", "", "指定版本；留空回滚 previous")
		if parseErr := fs.Parse(cmdArgs); parseErr != nil {
			return 2
		}
		err = engine.Rollback(ctx, *ver)
	case "cleanup":
		err = engine.Cleanup(ctx)
	default:
		log.Printf("未知命令: %s", cmd)
		usage()
		return 2
	}
	if err != nil {
		log.Printf("%s 失败: %v", cmd, err)
		return 1
	}
	log.Printf("%s 完成", cmd)
	return 0
}

func usage() {
	fmt.Fprint(os.Stderr, `SimpleWebShell-RemoteUpdate - 非侵入式远程更新工作流

用法:
  simplewebshell-remoteupdate [全局参数] <命令> [命令参数]

全局参数:
  -config remoteupdate.json   JSON 配置文件
  -dry-run                    打印远程命令但不执行变更
  -version                    显示版本

命令:
  probe                       验证 SimpleWebShell 连通性与密钥
  init                        初始化远端版本目录
  install -artifact FILE [-release VERSION]
                              首次安装，已有 current 时拒绝覆盖
  update -artifact FILE [-release VERSION]
                              安装新版本并原子切换 current
  status                      查看 current / previous / releases
  rollback [-release VERSION] 回滚 previous 或指定版本
  cleanup                     清理旧的非活动版本

密钥建议通过 SIMPLEWEBSHELL_KEY 环境变量传入。
`)
}
