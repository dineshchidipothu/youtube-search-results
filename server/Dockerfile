FROM golang:latest

COPY . /go/src/app

WORKDIR /go/src/app

RUN go build -o main .

CMD ["./main"]
