# Youtube search aggregation

This project involves two components

1. Worker
1. Server

### Worker

Does the following once every time in a pre-defined polling interval

- Fetches youtube video details for a specific search term using youtube search api
- Stores the results into the mongo database.

  1. A collection is created for the search term if it doesn't exist
  1. Adds required indexes to for the created collection:
     - publishedAt: indexed with descending order
     - title, description: text index for search functionality
     - youtubeID: unique index to ensure we don't store duplicates

  This happens asynchronously so the polling wait isn't effected.

#### Requires the following env variables:

```
API_KEY=<google api key to access youtube search api>
POLL_INTERVAL=<how often do the search>
MONGO_DB=<db name>
MONGO_URI=<uri to connect to db. eg: mongodb://mongodb:27017>
```

### Server

Responsible for serving the data collected by Worker.
Currently, only supports the following api:
`GET /videos/<searchTerm>` // `searchTerm` is the one that worker has collected data for. The server uses it to get the collection from DB.
It supports the following query params:
| param  | required | description                                                                                                                       |
|--------|----------|-----------------------------------------------------------------------------------------------------------------------------------|
| page   | no       | The page number. Defaults to 0                                                                                                    |
| limit  | no       | Max number of results to send. Defaults to 10                                                                                     |
| search | no       | Acts as basic search. Queries the database for the documents containing the `search` words in title and description of the video. |

#### Response:
```
{
    "page": <page sent in the request>,
    "limit": <limit sent in the request>,
    "result": [ // List of videos
        {
            "_id": "<mongo object id>"
            "youtubeId": "<unique identified from youtube>"
            "title": "<video title>"
            "description": "<video description>"
            "publishedAt": "<video published time>"
            "thumbnailUrl": "<Default thumbnail's URL>"
        },
        .
        .
        .
    ],
    "prev": "<previous page url, if exists>",
    "next": "<next page url, if exists">,
}
```

#### Example request
```
curl "localhost:8080/videos/swimming?limit=3&search=beginner%20lessons"
```

#### Requires the following env variables:

```
MONGO_DB=<db name>
MONGO_URI=<uri to connect to db. eg: mongodb://mongodb:27017>
```

## Running locally
Add required env variables to `worker/.env` and `server/.env`, then run
`docker compose up`.

Change the search term in docker-compose.yaml to query youtube
for a specify search term. I've currently set it to 'music'.

Ensure docker is installed.

## Why two separate services?
* Having the distinction between worker and server helps keeps the project maintainable and scalable in the long run.
* Worker can be scaled up as required if the needs extend to multiple search terms.
* Can have many parallel workers gathering data for different sets of search terms with different api keys helping with not reaching daily api limit.
* High number of requests won't affect worker process
* server can also be scaled as per number of requests. 

## What can be further improved?
A few things can be further improved that I couldn't get to
* A common package with structs and utility functions like db connections etc so we don't have duplicate code in worker and server.
* Reserve main.go only for initialising the worker/server process. Have a `/pkg` in each so that it is easier to extend the code with more features.
* auth and ratelimit on the server requests.

