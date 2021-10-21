package cmd

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"regexp"

	applicationctl "g.hz.netease.com/horizon/core/controller/application"
	templatectl "g.hz.netease.com/horizon/core/controller/template"
	"g.hz.netease.com/horizon/core/http/api/v1/application"
	"g.hz.netease.com/horizon/core/http/api/v1/environment"
	"g.hz.netease.com/horizon/core/http/api/v1/group"
	"g.hz.netease.com/horizon/core/http/api/v1/member"
	"g.hz.netease.com/horizon/core/http/api/v1/template"
	"g.hz.netease.com/horizon/core/http/api/v1/user"
	"g.hz.netease.com/horizon/core/http/health"
	"g.hz.netease.com/horizon/core/http/metrics"
	metricsmiddle "g.hz.netease.com/horizon/core/middleware/metrics"
	usermiddle "g.hz.netease.com/horizon/core/middleware/user"
	"g.hz.netease.com/horizon/lib/orm"
	"g.hz.netease.com/horizon/pkg/application/gitrepo"
	gitlabfty "g.hz.netease.com/horizon/pkg/gitlab/factory"
	"g.hz.netease.com/horizon/pkg/server/middleware"
	"g.hz.netease.com/horizon/pkg/server/middleware/auth"
	logmiddle "g.hz.netease.com/horizon/pkg/server/middleware/log"
	ormmiddle "g.hz.netease.com/horizon/pkg/server/middleware/orm"
	"g.hz.netease.com/horizon/pkg/server/middleware/requestid"
	templateschema "g.hz.netease.com/horizon/pkg/templaterelease/schema"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v2"
)

// Flags defines agent CLI flags.
type Flags struct {
	ConfigFile string
	Dev        bool
}

// ParseFlags parses agent CLI flags.
func ParseFlags() *Flags {
	var flags Flags

	flag.StringVar(
		&flags.ConfigFile, "config", "", "configuration file path")

	flag.BoolVar(
		&flags.Dev, "dev", false, "if true, turn off the usermiddleware to skip login")

	flag.Parse()
	return &flags
}

// Run runs the agent.
func Run(flags *Flags) {
	var config Config
	// load config
	data, err := ioutil.ReadFile(flags.ConfigFile)
	if err != nil {
		panic(err)
	}

	if err := yaml.Unmarshal(data, &config); err != nil {
		panic(err)
	}

	// init db
	mysqlDB, err := orm.NewMySQLDB(&orm.MySQL{
		Host:              config.DBConfig.Host,
		Port:              config.DBConfig.Port,
		Username:          config.DBConfig.Username,
		Password:          config.DBConfig.Password,
		Database:          config.DBConfig.Database,
		PrometheusEnabled: config.DBConfig.PrometheusEnabled,
	})
	if err != nil {
		panic(err)
	}

	// init service
	ctx := orm.NewContext(context.Background(), mysqlDB)
	gitlabFactory := gitlabfty.NewFactory(config.GitlabMapper)
	applicationGitRepo, err := gitrepo.NewApplicationGitlabRepo(ctx, config.GitlabRepoConfig, gitlabFactory)
	if err != nil {
		panic(err)
	}
	templateSchemaGetter, err := templateschema.NewSchemaGetter(ctx, gitlabFactory)
	if err != nil {
		panic(err)
	}

	var (
		// init controller
		applicationCtl = applicationctl.NewController(applicationGitRepo, templateSchemaGetter)
		templateCtl    = templatectl.NewController(templateSchemaGetter)
	)

	var (
		// init API
		groupAPI       = group.NewAPI()
		templateAPI    = template.NewAPI(templateCtl)
		userAPI        = user.NewAPI()
		applicationAPI = application.NewAPI(applicationCtl)
		environmentAPI = environment.NewAPI()
		memberAPI      = member.NewAPI()
	)

	// init server
	r := gin.New()
	// use middleware
	middlewares := []gin.HandlerFunc{
		gin.LoggerWithWriter(gin.DefaultWriter, "/health", "/metrics"),
		gin.Recovery(),
		requestid.Middleware(),        // requestID middleware, attach a requestID to context
		logmiddle.Middleware(),        // log middleware, attach a logger to context
		ormmiddle.Middleware(mysqlDB), // orm db middleware, attach a db to context
		auth.Middleware(middleware.MethodAndPathSkipper("*",
			regexp.MustCompile("^/apis/[^c][^o][^r][^e].*"))),
		metricsmiddle.Middleware( // metrics middleware
			middleware.MethodAndPathSkipper("*", regexp.MustCompile("^/health")),
			middleware.MethodAndPathSkipper("*", regexp.MustCompile("^/metrics"))),
	}
	// enable usermiddle when current env is not dev
	if !flags.Dev {
		middlewares = append(middlewares,
			usermiddle.Middleware(config.OIDCConfig, //  user middleware, check user and attach current user to context.
				middleware.MethodAndPathSkipper("*", regexp.MustCompile("^/health")),
				middleware.MethodAndPathSkipper("*", regexp.MustCompile("^/metrics"))),
		)
	}
	r.Use(middlewares...)

	gin.ForceConsoleColor()

	// register routes
	health.RegisterRoutes(r)
	metrics.RegisterRoutes(r)
	group.RegisterRoutes(r, groupAPI)
	template.RegisterRoutes(r, templateAPI)
	user.RegisterRoutes(r, userAPI)
	application.RegisterRoutes(r, applicationAPI)
	environment.RegisterRoutes(r, environmentAPI)
	member.RegisterRoutes(r, memberAPI)

	log.Printf("Server started")
	log.Fatal(r.Run(fmt.Sprintf(":%d", config.ServerConfig.Port)))
}
