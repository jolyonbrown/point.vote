FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pointvote ./cmd/pointvote

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /pointvote /pointvote
EXPOSE 8080
ENTRYPOINT ["/pointvote", "-addr", "0.0.0.0:8080"]
