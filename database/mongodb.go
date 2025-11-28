package database

import (
	"context"
	"time"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
		config.Database = "corego"
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

func (m *MongoDB) InsertOne(collection string, document any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	result, err := db.Collection(collection).InsertOne(ctx, document)
	if err != nil {
		return "", err
	}

	// Convert inserted ID to string
	if oid, ok := result.InsertedID.(primitive.ObjectID); ok {
		return oid.Hex(), nil
	}

	return "", nil
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

func (m *MongoDB) Find(collection string, filter any) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db := m.client.Database(m.config.Database)
	cursor, err := db.Collection(collection).Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []map[string]any
	if err = cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return results, nil
}

func (m *MongoDB) Collection(name string) *mongo.Collection {
	return m.client.Database(m.config.Database).Collection(name)
}