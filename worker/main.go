package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"google.golang.org/api/googleapi/transport"
	"google.golang.org/api/youtube/v3"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Video struct {
	ID           primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	YoutubeID    string             `json:"youtubeId,omitempty" bson:"youtubeId,omitempty"`
	Title        string             `json:"title,omitempty" bson:"title,omitempty"`
	Description  string             `json:"description,omitempty" bson:"description,omitempty"`
	PublishedAt  time.Time          `json:"publishedAt,omitempty" bson:"publishedAt,omitempty"`
	ThumbnailUrl string             `json:"thumbnailUrl,omitempty" bson:"thumbnailUrl,omitempty"`
}

func handleError(err error) {
	fmt.Printf("Error: %+v", err)
}

type Service struct {
	youtubeClient       *youtube.Service
	mongoClient         *mongo.Client
	database            *mongo.Database
	existingCollections []string
}

func New(ctx context.Context, apiKey, mongoUri, mongoDbName string) *Service {
	httpClient := &http.Client{
		Transport: &transport.APIKey{Key: apiKey},
	}

	youtubeClient, err := youtube.New(httpClient)
	if err != nil {
		log.Fatalf("Error creating new YouTube client: %v", err)
	}

	mongoOptions := options.Client().ApplyURI(mongoUri)
	mongoClient, err := mongo.Connect(ctx, mongoOptions)
	if err != nil {
		log.Fatalf("Error: Mongo connection failed: %v", err)
	}

	err = mongoClient.Ping(ctx, nil)
	if err != nil {
		log.Fatalf("Error: Database ping failed: %v", err)
	}
	log.Println("MongoDB connection successful!")

	database := mongoClient.Database(mongoDbName)

	return &Service{
		youtubeClient: youtubeClient,
		mongoClient:   mongoClient,
		database:      database,
	}
}

func (s *Service) fetchVideos(searchKey string, since time.Time) []interface{} {
	if s.youtubeClient == nil {
		log.Println("Error: youtubeClient not initialised")
		return nil
	}

	call := s.youtubeClient.Search.List([]string{"id", "snippet"}).
		Q(searchKey).
		Type("video").
		PublishedAfter(since.Format(time.RFC3339)).
		MaxResults(50)
	response, err := call.Do()
	if err != nil {
		log.Printf("Error: Unable to get search results: %v", err)
	}

	var videos []interface{}
	for _, item := range response.Items {
		v := Video{
			YoutubeID:    item.Id.VideoId,
			Title:        item.Snippet.Title,
			Description:  item.Snippet.Description,
			ThumbnailUrl: item.Snippet.Thumbnails.Default.Url,
		}
		publishedAt, err := time.Parse(time.RFC3339, item.Snippet.PublishedAt)
		if err != nil {
			log.Println("Error: Unable to parse PublishedAt field")
		} else {
			v.PublishedAt = publishedAt
		}
		videos = append(videos, v)
	}
	return videos
}

func keywordExistsIn(keyword string, list []string) bool {
	// TODO: optimise this search
	for _, c := range list {
		if c == keyword {
			return true
		}
	}
	return false
}

func (s *Service) collectionExists(ctx context.Context, collection string) bool {
	if keywordExistsIn(collection, s.existingCollections) {
		return true
	}
	// Update existing collections & check again, in case new ones were added
	collections, err := s.database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		log.Printf("Error: Unable get collections list")
		return false
	}
	s.existingCollections = collections
	return keywordExistsIn(collection, collections)
}

// createIndexes adds these indexes on collection:
// Single field Index on PublishedAt to keep docs in reverse chronological order
// Text Index on Title and Description for search
// Unique Index on YoutubeId so we don't add duplicates
func (s *Service) createIndexes(ctx context.Context, collection *mongo.Collection) {
	publishedAtIndex := mongo.IndexModel{Keys: bson.D{{"publishedAt", -1}}}
	textIndex := mongo.IndexModel{Keys: bson.D{
		{"title", "text"},
		{"description", "text"},
	}}
	youtubeIdIndex := mongo.IndexModel{
		Keys:    bson.D{{"youtubeId", 1}},
		Options: options.Index().SetUnique(true),
	}
	indexes := collection.Indexes()
	names, err := indexes.CreateMany(ctx, []mongo.IndexModel{publishedAtIndex, textIndex, youtubeIdIndex})
	if err != nil {
		log.Println("Error: Failed to create indexes")
		return
	}
	log.Printf("Successfully created indexes: %v", names)
}

func (s *Service) saveVideosToDB(ctx context.Context, searchKey string, videos []interface{}) {
	collectionPreviouslyExists := s.collectionExists(ctx, searchKey)
	collection := s.database.Collection(searchKey)
	if !collectionPreviouslyExists {
		s.createIndexes(ctx, collection)
	}

	_, err := collection.InsertMany(ctx, videos, options.InsertMany().SetOrdered(false))
	if err != nil {
		// This could be triggered when inserting duplicates but shouldn't be a problem
		// as other values are inserted with ordered set to false.
		log.Printf("Error: DB update failed: %v", err)
		return
	}
	log.Printf("Inserted %d documents to db", len(videos))
}

func main() {
	if len(os.Args) == 1 {
		log.Fatal("Missing search term, send as argument")
	}
	searchTerm := os.Args[1]

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		log.Fatal("Missing API_KEY")
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://0.0.0.0:27017"
	}

	mongoDbName := os.Getenv("MONGO_DB")
	if mongoDbName == "" {
		log.Fatal("MONGO_DB missing")
	}

	pollInterval, err := strconv.Atoi(os.Getenv("POLL_INTERVAL"))
	if err != nil {
		pollInterval = 10
		log.Printf("Unable to set polling interval. Defaulting to %d seconds", pollInterval)
	}

	ctx := context.Background()
	s := New(ctx, apiKey, mongoURI, mongoDbName)

	var lastFetchedTime time.Time
	for {
		videos := s.fetchVideos(searchTerm, lastFetchedTime)
		numVideos := len(videos)
		log.Println("FETCHED:", numVideos)
		if numVideos != 0 {
			go s.saveVideosToDB(ctx, searchTerm, videos)
		}
		lastFetchedTime = time.Now()
		time.Sleep(time.Duration(pollInterval) * time.Second)
	}
}
