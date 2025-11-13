package database

import (
	"context"
	"time"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoDB struct {
	client		*mongo.Client
	config		*MongoConfig
}

func NewMongoDB(config *MongoConfig) (*MongoDB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().ApplyURI(config.URL)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}

	if config.Database == "" {
		config.Database = "gocore"
	}

	return &MongoDB{
		client: client,
		config: config,
	}, nil
}

func (m *MongoDB) GetClient() *mongo.Client {
	return m.client
}

func (m *MongoDB) Disconnect() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return m.client.Disconnect(ctx)
}

func (m *MongoDB) InsertOne(collection string, document any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	_, err := db.Collection(collection).InsertOne(ctx, document)
	return err
}

func (m *MongoDB) FindOne(collection string, filter any, result any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	return db.Collection(collection).FindOne(ctx, filter).Decode(result)
}

func (m *MongoDB) DeleteOne(collection string, filter any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	_, err := db.Collection(collection).DeleteOne(ctx, filter)
	return err
}

func (m *MongoDB) DeleteMany(collection string, filter any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	_, err := db.Collection(collection).DeleteMany(ctx, filter)
	return err
}

func (m *MongoDB) UpdateOne(collection string, filter any, update any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	_, err := db.Collection(collection).UpdateOne(ctx, filter, update)
	return err
}

func (m *MongoDB) UpdateMany(collection string, filter, update any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	_, err := db.Collection(collection).UpdateMany(ctx, filter, update)
	return err
}