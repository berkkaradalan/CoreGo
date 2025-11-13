package gocore

import (
	"github.com/berkkaradalan/GoCore/database"
	"github.com/berkkaradalan/GoCore/env"
)

type Config struct {
	Mongo *database.MongoConfig
}

type Core struct {
	Env 		*env.Env
	Mongo		*database.MongoDB
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

	return core, nil
}

func (c *Core) Close() error {
	if c.Mongo != nil {
		return c.Mongo.Disconnect()
	}
	return nil
}