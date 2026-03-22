# Build API server (static binary, no CGO).
FROM golang:1.24-alpine AS build
WORKDIR /src

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /

COPY --from=build /out/server /server
# Static assets only (exclude package frontend’s paths.go from the runtime image).
COPY --from=build /src/cmd/server/frontend/index.html /frontend/index.html
COPY --from=build /src/cmd/server/frontend/styles.css /frontend/styles.css
COPY --from=build /src/cmd/server/frontend/app.js /frontend/app.js

ENV FRONTEND_DIR=/frontend
EXPOSE 8085

ENTRYPOINT ["/server"]
