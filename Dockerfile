FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pg-tileserver ./cmd/pg-tileserver

FROM gcr.io/distroless/static-debian12
COPY --from=build /pg-tileserver /pg-tileserver
EXPOSE 7800
ENTRYPOINT ["/pg-tileserver"]
