package corego

import (
	"github.com/berkkaradalan/CoreGo/auth"
	"github.com/berkkaradalan/CoreGo/database"
	"github.com/berkkaradalan/CoreGo/env"
)

type Config struct {
	Mongo 		*database.MongoConfig
	Postgres 	*database.PostgresConfig
	Auth  		*auth.Config
}

type Core struct {
	Env 		*env.Env
	Mongo		*database.MongoDB
	Postgres	*database.PostgresDB
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

	if config.Postgres != nil {
		postgres, err := database.NewPostgresDB(config.Postgres)
		if err != nil {
			return nil, err
		}
		core.Postgres = postgres
	} else if core.Env.POSTGRES_CONNECTION_URL != nil {
		postgres, err := database.NewPostgresDB(&database.PostgresConfig{
			URL: *core.Env.POSTGRES_CONNECTION_URL,
		})
		if err != nil {
			return nil, err
		}
		core.Postgres = postgres
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