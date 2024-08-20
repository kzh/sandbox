package env

import (
	"github.com/spf13/viper"
)

const (
	Production  Environment = "prod"
	Development Environment = "dev"
)

type Environment string

func Env() Environment {
	return Environment(viper.GetString("environment"))
}
