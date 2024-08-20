package main

import (
	"log"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/kzh/sandbox/pkg/api"
	"github.com/kzh/sandbox/pkg/env"
)

func main() {
	cmd := &cobra.Command{
		RunE: func(cmd *cobra.Command, args []string) error {
			viper.SetEnvPrefix("sandbox_api")
			viper.AutomaticEnv()

			viper.BindEnv("DBUsername", "DBUSERNAME")
			viper.BindEnv("DBPassword", "DBPASSWORD")

			viper.SetDefault("address", "0.0.0.0:3001")
			viper.SetDefault("dburl", "postgres://localhost:5432/sandbox?sslmode=disable")
			viper.SetDefault("environment", "dev")

			config := &api.ServerConfig{}
			if err := viper.Unmarshal(config); err != nil {
				return errors.Wrap(err, "failed to unmarshal config")
			}

			var logger *zap.Logger
			switch env.Env() {
			case env.Production:
				logger = zap.Must(zap.NewProduction())
			case env.Development:
				logger = zap.Must(zap.NewDevelopment())
			}
			zap.ReplaceGlobals(logger)

			api.Start(config)
			return nil
		},
	}
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
