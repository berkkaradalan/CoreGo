package gocore

import (
	"github.com/berkkaradalan/GoCore/env"
)

type Core struct {
	Env *env.Env
}

func New() *Core{
	envConfig := env.LoadEnv()

	return &Core{
		Env: envConfig,
	}
}