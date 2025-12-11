package env

import (
	"log"
	"os"
	"github.com/joho/godotenv"
)

type Env struct {
	URL							string `mapstructure:"URL"`
	PORT						string `mapstructure:"PORT"`
	MONGODB_CONNECTION_URL		*string `mapstructure:"MONGODB_CONNECTION_URL"`
	POSTGRES_CONNECTION_URL		*string `mapstructure:"POSTGRES_CONNECTION_URL"`
	MANUAL 						map[string]string
}

func LoadEnv() (*Env){
	err := godotenv.Load(".env")

	if err != nil {
		log.Printf("Warning: .env file not found, using defaults.")
	}

	url := os.Getenv("URL")
    port := os.Getenv("PORT")

	//Default Values
	if url == "" {
        url = "http://localhost"
    }
    if port == "" {
        port = "8080"
    }

	//Optional Values
	var mongoURL *string
	if mongoEnv := os.Getenv("MONGODB_CONNECTION_URL"); mongoEnv != "" {
		mongoURL = &mongoEnv
	}

	return &Env{
		URL: url,
		PORT: port,
		MONGODB_CONNECTION_URL: mongoURL,
		MANUAL: make(map[string]string),
	}
}