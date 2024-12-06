# Use the official Golang image to create a build artifact.
FROM golang:1.23

# Set the working directory to /app
WORKDIR /app

# Copy the current directory contents into the container at /app
COPY go.mod go.sum main.go japan.geojson ./

RUN go mod download && \
  go build -o main /app/main.go

# Run the binary program produced by `go build`
CMD [ "/app/main" ]