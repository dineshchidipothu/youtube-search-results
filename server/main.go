package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	defaultLimit = 10
	maxLimit     = 50
)

type Error struct {
	Code    int
	Message string
}

func (e *Error) writeHttpResponse(w http.ResponseWriter) {
	http.Error(w, e.Message, e.Code)
}

var (
	database            *mongo.Database
	existingCollections []string
	pageRegex           = regexp.MustCompile(`page=[0-9]*`)

	internalError = Error{http.StatusInternalServerError, "Internal error"}
)

// TODO: Don't duplicate, import
type Video struct {
	ID           primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	YoutubeID    string             `json:"youtubeId,omitempty" bson:"youtubeId,omitempty"`
	Title        string             `json:"title,omitempty" bson:"title,omitempty"`
	Description  string             `json:"description,omitempty" bson:"description,omitempty"`
	PublishedAt  time.Time          `json:"publishedAt,omitempty" bson:"publishedAt,omitempty"`
	ThumbnailUrl string             `json:"thumbnailUrl,omitempty" bson:"thumbnailUrl,omitempty"`
}

func setupDatabaseConnection(ctx context.Context, mongoUri, mongoDbName string) {
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

	database = mongoClient.Database(mongoDbName)
}

type videosResponseMsg struct {
	Page   int     `json:"page"`
	Limit  int     `json:"limit"`
	Result []Video `json:"result"`
	Prev   string  `json:"prev"`
	Next   string  `json:"next"`
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

// validateKeyword ensures the relevant collection exists.
func validateKeyword(ctx context.Context, keyword string) *Error {
	if keywordExistsIn(keyword, existingCollections) {
		return nil
	}
	collections, err := database.ListCollectionNames(ctx, bson.D{})
	if err != nil {
		log.Println("Error: Unable to get list of collections")
		return &internalError
	}
	existingCollections = collections
	if keywordExistsIn(keyword, existingCollections) {
		return nil
	}
	return &Error{http.StatusBadRequest, fmt.Sprintf("Videos for %s are not being collected", keyword)}
}

func getVideos(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Path[len("/videos/"):]
	if err := validateKeyword(r.Context(), keyword); err != nil {
		err.writeHttpResponse(w)
		return
	}

	q := r.URL.Query()
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil {
		page = 0
	}

	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil {
		limit = defaultLimit
	}
	if limit > maxLimit {
		log.Printf("Error: limit exceeded")
	}

	search := q.Get("search")

	skip := page * limit
	// limit+1, so we know if next exists
	findOptions := options.Find().SetSkip(int64(skip)).SetLimit(int64(limit + 1)).SetSort(bson.D{{"publishedAt", -1}})
	filter := bson.D{}
	if search != "" {
		// Question: Should this be full search?
		filter = bson.D{{Key: "$text", Value: bson.D{{Key: "$search", Value: search}}}}
	}

	collection := database.Collection(keyword)
	cursor, err := collection.Find(r.Context(), filter, findOptions)
	if err != nil {
		log.Printf("Error: cannot get videos: %v", err)
		internalError.writeHttpResponse(w)
		return
	}
	defer cursor.Close(r.Context())

	var videos []Video
	next := ""
	i := 1
	for cursor.Next(r.Context()) {
		if i > limit {
			nextReq := *r
			q := nextReq.URL.Query()
			q.Set("page", strconv.Itoa(page+1))
			nextReq.URL.RawQuery = q.Encode()
			next = nextReq.Host + nextReq.URL.String()
		}
		var v Video
		if err := cursor.Decode(&v); err != nil {
			log.Println("Error: failed to decode result")
			continue
		}
		videos = append(videos, v)
		i++
	}
	response := videosResponseMsg{
		Page:   page,
		Limit:  limit,
		Result: videos,
	}
	if next != "" {
		response.Next = next
	}
	if page != 0 {
		prevReq := *r
		q := prevReq.URL.Query()
		q.Set("page", strconv.Itoa(page-1))
		prevReq.URL.RawQuery = q.Encode()
		response.Prev = prevReq.Host + prevReq.URL.String()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://0.0.0.0:27017"
	}
	mongoDbName := os.Getenv("MONGO_DB")
	if mongoDbName == "" {
		log.Fatal("MONGO_DB missing")
	}
	setupDatabaseConnection(context.Background(), mongoURI, mongoDbName)
	http.HandleFunc("/videos/", getVideos)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
