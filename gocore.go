package gocore

import (
	"github.com/berkkaradalan/GoCore/auth"
	"github.com/berkkaradalan/GoCore/database"
	"github.com/berkkaradalan/GoCore/env"
)

type Config struct {
	Mongo *database.MongoConfig
	Auth  *auth.Config
}

type Core struct {
	Env 		*env.Env
	Mongo		*database.MongoDB
	Auth		*auth.Manager
}

func New(config *Config) (*Core, error){
	core := &Core{
		Env: env.LoadEnv(),
	}

	if config == nil {
		config = &Config{}
	}

	if config.Mongo != nil {
		mongo, err := database.NewMongoDB(config.Mongo)
		if err != nil {
			return nil, err
		}
		core.Mongo = mongo
	} else if core.Env.MONGODB_CONNECTION_URL != nil {
		mongo, err := database.NewMongoDB(&database.MongoConfig{
			URL : *core.Env.MONGODB_CONNECTION_URL,
		})
		if err != nil {
			return nil, err
		}
		core.Mongo = mongo
	}

	// Initialize Auth if config provided and MongoDB is available
	if config.Auth != nil && core.Mongo != nil {
		authManager, err := auth.New(config.Auth, core.Mongo)
		if err != nil {
			return nil, err
		}
		core.Auth = authManager
	}

	return core, nil
}

func (c *Core) Close() error {
	if c.Mongo != nil {
		return c.Mongo.Disconnect()
	}
	return nil
}