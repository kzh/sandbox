package api

import (
	"context"
	"net/http"
	"net/url"
	"time"

	ratelimit "github.com/JGLTechnologies/gin-rate-limit"
	"github.com/cockroachdb/errors"
	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"

	"github.com/kzh/sandbox/pkg/env"
	"github.com/kzh/sandbox/pkg/executor"
)

type ServerConfig struct {
	Address    string
	DBURL      string
	DBUsername string
	DBPassword string
}

type Server struct {
	config *ServerConfig
	pool   *pgxpool.Pool

	executor *executor.Service
}

func database(config *ServerConfig) (*pgxpool.Pool, error) {
	u, err := url.Parse(config.DBURL)
	if err != nil {
		return nil, errors.Wrap(err, "parse database url")
	}
	if config.DBUsername != "" || config.DBPassword != "" {
		u.User = url.UserPassword(config.DBUsername, config.DBPassword)
	}

	zap.S().Infof("%#v", config)

	pool, err := pgxpool.New(context.Background(), u.String())
	if err != nil {
		return nil, errors.Wrap(err, "new db connection pool")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		return nil, errors.Wrap(err, "ping db")
	}

	return pool, nil
}

func Start(config *ServerConfig) {
	fx.New(
		fx.Supply(config),
		fx.Provide(
			database,
			NewServer,
		),
		fx.WithLogger(func() fxevent.Logger {
			return &fxevent.ZapLogger{Logger: zap.L()}
		}),
		fx.Invoke(func(server *Server) {}),
	).Run()
}

type ServerParams struct {
	fx.In

	Config *ServerConfig
	Pool   *pgxpool.Pool
}

func NewServer(lc fx.Lifecycle, params ServerParams) *Server {
	exec, err := executor.NewService(context.Background(), "sandbox")
	if err != nil {
		zap.L().Panic("failed to create executor", zap.Error(err))
	}

	s := &Server{params.Config, params.Pool, exec}
	lc.Append(fx.Hook{
		OnStart: func(context context.Context) error {
			zap.L().Info("starting sandbox api server")
			return s.Start()
		},
	})
	return s
}

func (s *Server) Start() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(ginzap.Ginzap(zap.L(), time.RFC3339, true))
	router.Use(ginzap.RecoveryWithZap(zap.L(), true))

	api := router.Group("api")
	if env.Env() == env.Production {
		for rate, limit := range map[time.Duration]uint{
			time.Second: 1,
			time.Hour:   200,
		} {
			api.Use(ratelimit.RateLimiter(ratelimit.InMemoryStore(&ratelimit.InMemoryOptions{
				Rate:  rate,
				Limit: limit,
			}), nil))
		}
	}
	api.POST("execute", s.handleExecute)

	go router.Run(s.config.Address)
	return nil
}

type ExecuteParams struct {
	Code string `json:"code"`
}

func (s *Server) handleExecute(c *gin.Context) {
	var params ExecuteParams
	if err := c.ShouldBindJSON(&params); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{})
		return
	}

	zap.L().Info("received request", zap.String("code", params.Code))

	out, err := s.executor.Execute(context.Background(), params.Code)
	if err != nil {
		zap.L().Error("failed to execute code", zap.Error(err))
		c.JSON(200, gin.H{
			"output": err.Error(),
		})
		return
	}

	zap.L().Info("completed request", zap.String("code", params.Code))

	c.JSON(200, gin.H{
		"output": out,
	})
}
