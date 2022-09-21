package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"g.hz.netease.com/horizon/core/config"
	clusterctl "g.hz.netease.com/horizon/core/controller/cluster"
	environmentctl "g.hz.netease.com/horizon/core/controller/environment"
	prctl "g.hz.netease.com/horizon/core/controller/pipelinerun"
	userctl "g.hz.netease.com/horizon/core/controller/user"
	"g.hz.netease.com/horizon/core/http/health"
	ginlogmiddle "g.hz.netease.com/horizon/core/middleware/ginlog"
	"g.hz.netease.com/horizon/job/autofree"
	"g.hz.netease.com/horizon/lib/orm"
	"g.hz.netease.com/horizon/pkg/cluster/cd"
	"g.hz.netease.com/horizon/pkg/grafana"
	"g.hz.netease.com/horizon/pkg/param"
	"g.hz.netease.com/horizon/pkg/param/managerparam"
	"g.hz.netease.com/horizon/pkg/util/kube"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// Flags defines agent CLI flags.
type Flags struct {
	ConfigFile         string
	Environment        string
	LogLevel           string
	AutoReleaseAccount string
}

// ParseFlags parses agent CLI flags.
func ParseFlags() *Flags {
	var flags Flags

	flag.StringVar(
		&flags.ConfigFile, "config", "", "configuration file path")

	flag.StringVar(
		&flags.Environment, "environment", "production", "environment string tag")

	flag.StringVar(
		&flags.LogLevel, "loglevel", "info", "the loglevel(panic/fatal/error/warn/info/debug/trace))")

	flag.StringVar(
		&flags.AutoReleaseAccount, "autoreleaseaccount", "", "auto release cluster account")

	flag.Parse()
	return &flags
}

func InitLog(flags *Flags) {
	if flags.Environment == "production" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{})
	}
	logrus.SetOutput(os.Stdout)
	level, err := logrus.ParseLevel(flags.LogLevel)
	if err != nil {
		panic(err)
	}
	logrus.SetLevel(level)
}

// Run runs the agent.
func Run(flags *Flags) {
	// init log
	InitLog(flags)

	// load coreConfig
	coreConfig, err := config.LoadConfig(flags.ConfigFile)
	if err != nil {
		panic(err)
	}
	_, err = json.MarshalIndent(coreConfig, "", " ")
	if err != nil {
		panic(err)
	}

	// init db
	mysqlDB, err := orm.NewMySQLDB(&orm.MySQL{
		Host:              coreConfig.DBConfig.Host,
		Port:              coreConfig.DBConfig.Port,
		Username:          coreConfig.DBConfig.Username,
		Password:          coreConfig.DBConfig.Password,
		Database:          coreConfig.DBConfig.Database,
		PrometheusEnabled: coreConfig.DBConfig.PrometheusEnabled,
	})
	if err != nil {
		panic(err)
	}

	// init manager parameter
	manager := managerparam.InitManager(mysqlDB)
	// init context
	ctx := context.Background()

	parameter := &param.Param{
		Manager: manager,
		Cd:      cd.NewCD(coreConfig.ArgoCDMapper),
	}

	// init controller
	var (
		userCtl        = userctl.NewController(parameter)
		clusterCtl     = clusterctl.NewController(&config.Config{}, parameter)
		prCtl          = prctl.NewController(parameter)
		environmentCtl = environmentctl.NewController(parameter)
	)

	// init kube client
	_, client, err := kube.BuildClient("")
	if err != nil {
		panic(err)
	}

	// sync grafana datasource periodically
	grafanaService := grafana.NewService(coreConfig.GrafanaConfig, manager, client)
	cancellableCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		grafanaService.SyncDatasource(cancellableCtx)
	}()

	// automatically release expired clusters
	go func() {
		autofree.AutoReleaseExpiredClusterJob(cancellableCtx, flags.AutoReleaseAccount,
			userCtl, clusterCtl, prCtl, environmentCtl)
	}()

	r := gin.New()
	// use middleware
	middlewares := []gin.HandlerFunc{
		ginlogmiddle.Middleware(gin.DefaultWriter, "/health"),
		gin.Recovery(),
	}
	r.Use(middlewares...)

	gin.ForceConsoleColor()

	health.RegisterRoutes(r)

	log.Printf("Server started")
	log.Print(r.Run(fmt.Sprintf(":%d", coreConfig.ServerConfig.Port)))
}
