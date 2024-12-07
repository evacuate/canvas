# Use the official Golang image to create a build artifact.
FROM golang:1.23

# Set the working directory to /app
WORKDIR /app

# Copy the rest of the application code to the container
COPY . .

RUN go mod download && \
  go build -o main /app/main.go

# Run the binary program produced by `go build`
CMD [ "/app/main" ]